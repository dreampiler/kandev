package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/task/models"
)

type supersessionDeliveryRepository struct {
	recordingAgentDeliveryRepository
	mu         sync.Mutex
	calls      []string
	compactErr error
}

// ReceiveAgentDeliveryEvent stamps the receive-time hash like the SQL inbox.
func (r *supersessionDeliveryRepository) ReceiveAgentDeliveryEvent(
	ctx context.Context, event *models.AgentDeliveryEvent, remoteHighWater int64,
) (bool, error) {
	event.ContentHash = fmt.Sprintf("hash-%d", event.Sequence)
	return r.recordingAgentDeliveryRepository.ReceiveAgentDeliveryEvent(ctx, event, remoteHighWater)
}

func (r *supersessionDeliveryRepository) CompactSupersededAgentDeliveryToolUpdates(
	_ context.Context, superseding *models.AgentDeliveryEvent, hints []models.AgentDeliverySupersededToolUpdate,
) (int, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	superseded := make([]int64, 0, len(hints))
	for _, hint := range hints {
		if hint.ContentHash != fmt.Sprintf("hash-%d", hint.Sequence) {
			return 0, 0, fmt.Errorf("hint %d carries hash %q", hint.Sequence, hint.ContentHash)
		}
		superseded = append(superseded, hint.Sequence)
	}
	if superseding.Payload != nil {
		return 0, 0, errors.New("queued superseding event pins its payload")
	}
	projected := false
	for _, event := range r.projected {
		if event.StreamID == superseding.StreamID && event.Sequence == superseding.Sequence {
			projected = true
		}
	}
	r.calls = append(r.calls, fmt.Sprintf("%d<-%v projected=%v", superseding.Sequence, superseded, projected))
	if r.compactErr != nil {
		return 0, 0, r.compactErr
	}
	return len(hints), 0, nil
}

func (r *supersessionDeliveryRepository) setCompactErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.compactErr = err
}

func (r *supersessionDeliveryRepository) recordedCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func waitSupersessionIdle(t *testing.T, sm *StreamManager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sm.toolUpdateSupersession.mu.Lock()
		idle := !sm.toolUpdateSupersession.running && len(sm.toolUpdateSupersession.queue) == 0
		sm.toolUpdateSupersession.mu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("superseded tool update compaction did not drain")
		}
		time.Sleep(time.Millisecond)
	}
}

func processSupersessionEvent(
	t *testing.T, sm *StreamManager, execution *AgentExecution, repository AgentDeliveryRepository,
	sequence uint64, eventType, toolCallID, status string,
) {
	t.Helper()
	event := agentctl.AgentEvent{
		Type: eventType, ToolCallID: toolCallID, ToolStatus: status, Text: "output",
		DeliveryStreamID: "stream-1", DeliveryIncarnationID: "incarnation-1",
		DeliveryHarnessGeneration: 1, DeliverySequence: sequence,
	}
	if err := sm.processAgentEvent(context.Background(), execution, nil, repository, event, 0); err != nil {
		t.Fatalf("process event %d: %v", sequence, err)
	}
	waitSupersessionIdle(t, sm)
}

func supersessionTestExecution() *AgentExecution {
	return &AgentExecution{
		SessionID: "session-1", DeliveryMode: DurableDeliveryV1,
		DeliveryStreamID: "stream-1", DeliveryIncarnationID: "incarnation-1", DeliveryHarnessGeneration: 1,
	}
}

func TestDurableToolUpdateProjectionHintsSupersededProgress(t *testing.T) {
	repository := &supersessionDeliveryRepository{}
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{}, nil, nil)
	defer sm.Wait()
	execution := supersessionTestExecution()

	processSupersessionEvent(t, sm, execution, repository, 1, streams.EventTypeToolCall, "exec-x", "in_progress")
	processSupersessionEvent(t, sm, execution, repository, 2, streams.EventTypeToolUpdate, "exec-x", "in_progress")
	processSupersessionEvent(t, sm, execution, repository, 3, streams.EventTypeToolUpdate, "exec-y", "in_progress")
	processSupersessionEvent(t, sm, execution, repository, 4, streams.EventTypeToolUpdate, "exec-x", "in_progress")
	processSupersessionEvent(t, sm, execution, repository, 5, streams.EventTypeToolUpdate, "exec-x", "complete")
	processSupersessionEvent(t, sm, execution, repository, 6, streams.EventTypeToolUpdate, "exec-x", "in_progress")

	// Only a projected later update of the same tool call supersedes; the
	// final status is never hinted, and the tool_call event is not a hint.
	want := []string{"4<-[2] projected=true", "5<-[4] projected=true"}
	if got := repository.recordedCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("superseded compaction calls = %v, want %v", got, want)
	}
}

func TestDurableToolUpdateSupersessionRetriesUncommittedHints(t *testing.T) {
	repository := &supersessionDeliveryRepository{compactErr: errors.New("database is locked")}
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{}, nil, nil)
	defer sm.Wait()
	execution := supersessionTestExecution()
	for sequence := uint64(1); sequence <= 3; sequence++ {
		if sequence == 3 {
			repository.setCompactErr(nil)
		}
		processSupersessionEvent(t, sm, execution, repository, sequence, streams.EventTypeToolUpdate, "exec-x", "in_progress")
	}
	want := []string{"2<-[1] projected=true", "3<-[1 2] projected=true"}
	if got := repository.recordedCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("superseded compaction calls = %v, want %v", got, want)
	}
	if got := len(repository.projected); got != 3 {
		t.Fatalf("projected events = %d, want 3", got)
	}
}

func TestToolUpdateSupersessionCoalescesQueuedJobs(t *testing.T) {
	var hints toolUpdateSupersession
	key := toolUpdateSupersessionKey{streamID: "stream-1", toolCallID: "exec-x"}
	compactor := &supersessionDeliveryRepository{}
	if !hints.schedule(key, compactor, models.AgentDeliveryEvent{Sequence: 3}, []supersededHint{{Sequence: 2, ContentHash: "h2"}}) {
		t.Fatal("first job did not request a drain")
	}
	if hints.schedule(key, compactor, models.AgentDeliveryEvent{Sequence: 4}, []supersededHint{{Sequence: 3, ContentHash: "h3"}}) {
		t.Fatal("queued job requested a second drain")
	}
	_, job, ok := hints.next()
	want := []supersededHint{{Sequence: 2, ContentHash: "h2"}, {Sequence: 3, ContentHash: "h3"}}
	if !ok || job.superseding.Sequence != 4 || !reflect.DeepEqual(job.hints, want) {
		t.Fatalf("coalesced job = %+v ok %v, want hints 2 and 3 behind 4", job, ok)
	}
	if _, _, ok := hints.next(); ok || hints.running {
		t.Fatal("drain stayed running after the queue emptied")
	}
}

func TestToolUpdateSupersessionHintsStayBounded(t *testing.T) {
	var hints toolUpdateSupersession
	for index := 0; index < maxSupersededToolCallHints+10; index++ {
		key := toolUpdateSupersessionKey{streamID: "stream-1", toolCallID: fmt.Sprintf("tool-%d", index)}
		hints.advance(key, supersededHint{Sequence: int64(index + 1), ContentHash: "h"}, true)
	}
	if got := len(hints.pending); got > maxSupersededToolCallHints {
		t.Fatalf("pending hints = %d, want at most %d", got, maxSupersededToolCallHints)
	}
	key := toolUpdateSupersessionKey{streamID: "stream-1", toolCallID: "long"}
	for sequence := int64(1); sequence <= 40; sequence++ {
		hints.restore(key, []supersededHint{{Sequence: sequence, ContentHash: "h"}})
	}
	if got := len(hints.pending[key]); got != maxSupersededSequencesPerToolCall {
		t.Fatalf("restored hints for one tool = %d, want %d", got, maxSupersededSequencesPerToolCall)
	}
}
