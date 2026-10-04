package statussummary

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/events"
)

type countingLaunchQueue struct {
	mu    sync.Mutex
	calls int
	queue *LaunchQueueSummary
}

func (l *countingLaunchQueue) load(context.Context, string) (*LaunchQueueSummary, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return cloneLaunchQueue(l.queue), nil
}

func (l *countingLaunchQueue) set(queue *LaunchQueueSummary) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queue = queue
}

func (l *countingLaunchQueue) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func testLaunchQueue(sessionID string) *LaunchQueueSummary {
	return &LaunchQueueSummary{
		SessionID: sessionID,
		QueuedAt:  time.Date(2026, 8, 1, 18, 0, 0, 0, time.UTC),
		Reason:    LaunchQueueReasonSessionCapacity,
		Retrying:  true,
	}
}

// TestProjectorDoesNotRereadLaunchQueueForPerMessageTraffic pins the read bound:
// a burst of the events one agent turn emits per tool update must not re-read the
// task row or the install-wide admission population. The previously observed
// queue is retained instead of being cleared.
func TestProjectorDoesNotRereadLaunchQueueForPerMessageTraffic(t *testing.T) {
	projector, store, eventBus, _, _ := newProjectorTest(t)
	loader := &countingLaunchQueue{queue: testLaunchQueue("session-1")}
	projector.loadLaunchQueue = loader.load

	const taskID = "task-message-traffic"
	publishSessionState(t, eventBus, taskID, "session-1", nil)
	baseline := loader.count()
	if baseline == 0 {
		t.Fatal("cold state must observe the launch queue once")
	}

	for i, eventType := range []string{
		events.MessageUpdated,
		events.MessageAdded,
		events.MessageDeleted,
		events.TaskSessionActivityChanged,
		events.TaskSessionErrorChanged,
		events.TurnStarted,
		events.TurnCompleted,
	} {
		publishProjectorEvent(t, eventBus, eventType, eventType, map[string]interface{}{
			"task_id":    taskID,
			"session_id": "session-1",
			"message_id": "message-1",
			"sequence":   i,
		})
	}

	if got := loader.count(); got != baseline {
		t.Fatalf("per-message traffic re-read the launch queue %d extra times", got-baseline)
	}

	loader.set(nil)
	publishProjectorEvent(t, eventBus, events.MessageUpdated, events.MessageUpdated, map[string]interface{}{
		"task_id":    taskID,
		"session_id": "session-1",
		"message_id": "message-2",
	})
	if queue := projectedQueue(projector, taskID); queue == nil || queue.SessionID != "session-1" {
		t.Fatalf("skipped source must retain the last observation, got %+v", queue)
	}
	publishProjectorEvent(t, eventBus, events.TaskUpdated, events.TaskUpdated, map[string]interface{}{
		"task_id": taskID,
	})
	if summary := store.summary(taskID); summary == nil || summary.LaunchQueue != nil {
		t.Fatalf("task-row event did not publish the cleared queue: %+v", summary)
	}
}

// TestProjectorRefreshesLaunchQueueForTaskAndSessionStateEvents pins the sources
// that do re-read: every task-row change and every session-state change, because
// those are what write a ceiling deferral or move the admitted population.
func TestProjectorRefreshesLaunchQueueForTaskAndSessionStateEvents(t *testing.T) {
	for _, eventType := range []string{
		events.TaskUpdated,
		events.TaskStateChanged,
		events.TaskSessionStateChanged,
		events.MessageQueueStatusChanged,
	} {
		t.Run(eventType, func(t *testing.T) {
			projector, _, eventBus, _, _ := newProjectorTest(t)
			loader := &countingLaunchQueue{queue: testLaunchQueue("session-1")}
			projector.loadLaunchQueue = loader.load

			const taskID = "task-refresh-source"
			publishProjectorEvent(t, eventBus, eventType, eventType, map[string]interface{}{
				"task_id":    taskID,
				"session_id": "session-1",
			})
			if loader.count() == 0 {
				t.Fatalf("%s did not refresh the launch queue", eventType)
			}
			queue := projectedQueue(projector, taskID)
			if queue == nil || queue.SessionID != "session-1" {
				t.Fatalf("%s did not project the launch queue: %+v", eventType, queue)
			}
		})
	}
}

// TestProjectorSurfacesDeferralRecordedBetweenMessageEvents keeps the queue
// correct when a ceiling deferral is written during streaming tool activity: the
// message traffic stays unbound, and the task-row event that follows publishes
// the new queue.
func TestProjectorSurfacesDeferralRecordedBetweenMessageEvents(t *testing.T) {
	projector, store, eventBus, _, _ := newProjectorTest(t)
	loader := &countingLaunchQueue{}
	projector.loadLaunchQueue = loader.load

	const taskID = "task-deferral-during-traffic"
	publishSessionState(t, eventBus, taskID, "session-1", nil)
	publishProjectorEvent(t, eventBus, events.MessageUpdated, events.MessageUpdated, map[string]interface{}{
		"task_id":    taskID,
		"session_id": "session-1",
	})
	if summary := store.summary(taskID); summary != nil && summary.LaunchQueue != nil {
		t.Fatalf("no deferral exists yet, got %+v", summary.LaunchQueue)
	}

	loader.set(testLaunchQueue("session-2"))
	publishProjectorEvent(t, eventBus, events.TaskUpdated, events.TaskUpdated, map[string]interface{}{
		"task_id": taskID,
	})
	summary := store.summary(taskID)
	if summary == nil || summary.LaunchQueue == nil || summary.LaunchQueue.SessionID != "session-2" {
		t.Fatalf("deferral written during tool traffic was not projected: %+v", summary)
	}
}

// TestProjectorObservesLaunchQueueOnColdStartFromAMessageEvent keeps the gate from
// skipping the one observation a task with no persisted summary still needs.
func TestProjectorObservesLaunchQueueOnColdStartFromAMessageEvent(t *testing.T) {
	projector, store, eventBus, _, _ := newProjectorTest(t)
	loader := &countingLaunchQueue{queue: testLaunchQueue("session-cold")}
	projector.loadLaunchQueue = loader.load

	const taskID = "task-cold-queue"
	publishProjectorEvent(t, eventBus, events.MessageUpdated, events.MessageUpdated, map[string]interface{}{
		"task_id":    taskID,
		"session_id": "session-cold",
	})
	if loader.count() == 0 {
		t.Fatal("a task with no persisted summary must observe its launch queue")
	}
	if queue := projectedQueue(projector, taskID); queue == nil || queue.SessionID != "session-cold" {
		t.Fatalf("cold observation was lost: %+v", queue)
	}

	loader.set(testLaunchQueue("session-next"))
	publishProjectorEvent(t, eventBus, events.TaskUpdated, events.TaskUpdated, map[string]interface{}{
		"task_id": taskID,
	})
	summary := store.summary(taskID)
	if summary == nil || summary.LaunchQueue == nil || summary.LaunchQueue.SessionID != "session-next" {
		t.Fatalf("changed queue was not published: %+v", summary)
	}
}

// TestIsLaunchQueueRefreshEventClassifiesEverySubscribedSource keeps the refresh
// set closed against the projector's own subscription list, so a newly
// subscribed source cannot silently skip the queue refresh.
func TestIsLaunchQueueRefreshEventClassifiesEverySubscribedSource(t *testing.T) {
	refresh := map[string]bool{
		events.TaskCreated:               true,
		events.TaskUpdated:               true,
		events.TaskStateChanged:          true,
		events.TaskSessionStateChanged:   true,
		events.MessageQueueStatusChanged: true,
	}
	for _, eventType := range []string{
		events.TaskCreated,
		events.TaskUpdated,
		events.TaskStateChanged,
		events.TaskSessionStateChanged,
		events.TaskSessionActivityChanged,
		events.TaskSessionErrorChanged,
		events.MessageAdded,
		events.MessageUpdated,
		events.MessageDeleted,
		events.TurnStarted,
		events.TurnCompleted,
		events.MessageQueueStatusChanged,
	} {
		want := refresh[eventType]
		if got := isLaunchQueueRefreshEvent(eventType); got != want {
			t.Errorf("isLaunchQueueRefreshEvent(%q) = %v, want %v", eventType, got, want)
		}
	}
	if isLaunchQueueRefreshEvent("task.deleted") {
		t.Error("an unsubscribed type must not be classified as a refresh source")
	}
}

// projectedQueue reads the projector-owned in-memory projection, which is where a
// source that changed nothing else is still visible before persistence.
func projectedQueue(projector *Projector, taskID string) *LaunchQueueSummary {
	projector.mu.Lock()
	defer projector.mu.Unlock()
	state := projector.state[taskID]
	if state == nil {
		return nil
	}
	return cloneLaunchQueue(state.launchQueue)
}
