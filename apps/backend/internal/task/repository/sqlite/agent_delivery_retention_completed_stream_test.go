package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/task/models"
)

// TestDeliveryRetentionCompletedStream proves the completed-stream predicate:
// a fully projected stream whose terminal is settled retires its fragment rows
// after the payload grace even while it is far inside the replay window, while
// an open stream keeps the existing age/count behavior.
func TestDeliveryRetentionCompletedStream(t *testing.T) {
	for _, engine := range []string{"SQLite", "Postgres"} {
		t.Run(engine+"/completed_stream_retires_within_window", func(t *testing.T) {
			repo, terminal, advance := seedRetentionPromptOutcome(
				t, engine, "completed-early", models.DeliverySubmissionCompleted,
			)
			ctx := context.Background()
			if _, err := repo.PutAgentDeliveryEffect(ctx, completedRetentionEventEffect(&terminal)); err != nil {
				t.Fatal(err)
			}
			putRetentionWorkflowOutcome(t, repo, terminal, "not_required", retentionPromptTurnID(t, terminal))
			if complete, err := repo.CompleteAgentDeliveryTerminalSettlement(ctx, terminalRetentionEffectKey(terminal), time.Time{}); err != nil || !complete {
				t.Fatalf("settlement: %v %v", complete, err)
			}
			successor := terminal
			successor.Sequence, successor.Terminal, successor.SubmissionID = 2, false, ""
			successor.EventType = streams.EventTypeMessageChunk
			successor.Payload, _ = json.Marshal(streams.AgentEvent{Type: successor.EventType, Text: "fragment after settled terminal"})
			if inserted, err := repo.ReceiveAgentDeliveryEvent(ctx, &successor, 2); err != nil || !inserted {
				t.Fatalf("receive successor: %v %v", inserted, err)
			}
			if _, err := repo.ProjectCanonicalAgentDeliveryEvent(ctx, &successor, completedRetentionEventEffect(&successor)); err != nil {
				t.Fatal(err)
			}
			// Past the payload grace, far inside the 24h / 10,000-event window.
			advance(2 * time.Hour)
			for attempt := 0; attempt < 3; attempt++ {
				if _, err := repo.RunAgentDeliveryRetentionBatch(ctx, models.AgentDeliveryRetentionBatchOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			cursor, err := repo.GetAgentDeliveryCursor(ctx, terminal.StreamID)
			if err != nil || cursor.RetiredSequence != 2 {
				t.Fatalf("completed stream frontier = %+v err=%v, want 2 (early retirement)", cursor, err)
			}
			summary, err := repo.GetAgentDeliveryEffect(ctx, terminalRetentionEffectKey(terminal))
			if err != nil || summary.State != models.DeliveryEffectCompleted {
				t.Fatalf("terminal receipt did not survive early retirement: %+v %v", summary, err)
			}
		})

		t.Run(engine+"/open_stream_keeps_window", func(t *testing.T) {
			repo, events, advance := seedRetentionOpenStream(t, engine, "open-window", 3)
			ctx := context.Background()
			advance(2 * time.Hour)
			for attempt := 0; attempt < 3; attempt++ {
				if _, err := repo.RunAgentDeliveryRetentionBatch(ctx, models.AgentDeliveryRetentionBatchOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			cursor, err := repo.GetAgentDeliveryCursor(ctx, events[0].StreamID)
			if err != nil {
				t.Fatalf("read open cursor: %v", err)
			}
			if cursor.RetiredSequence != 0 {
				t.Fatalf("open stream frontier = %d, want 0 (window still governs)", cursor.RetiredSequence)
			}
			var remaining int
			if err := repo.db.QueryRowx(repo.db.Rebind(`
				SELECT COUNT(*) FROM agent_delivery_inbox WHERE stream_id = ?`), events[0].StreamID).Scan(&remaining); err != nil {
				t.Fatalf("count open rows: %v", err)
			}
			if remaining != len(events) {
				t.Fatalf("open stream lost fragment rows: %d remaining, want %d", remaining, len(events))
			}
		})
	}
}

func seedRetentionOpenStream(
	t *testing.T,
	engine, suffix string,
	count int,
) (*Repository, []*models.AgentDeliveryEvent, func(time.Duration)) {
	t.Helper()
	repo := newRetentionTestRepo(t, engine)
	ctx := context.Background()
	baseTime := time.Date(2025, 7, 8, 9, 10, 11, 0, time.UTC)
	currentTime := baseTime
	repo.clockNow = func() time.Time { return currentTime }
	taskID := "retention-open-task-" + suffix
	sessionID := "retention-open-session-" + suffix
	streamID := "retention-open-stream-" + suffix
	if err := repo.CreateTask(ctx, &models.Task{
		ID: taskID, WorkspaceID: retentionTestWorkspaceID(t, repo), Title: "Retention open",
	}); err != nil {
		t.Fatalf("create open task: %v", err)
	}
	if err := repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: sessionID, TaskID: taskID, QueueIncarnationID: "incarnation-" + sessionID,
		State: models.TaskSessionStateRunning,
	}); err != nil {
		t.Fatalf("create open session: %v", err)
	}
	events := make([]*models.AgentDeliveryEvent, 0, count)
	for index := 0; index < count; index++ {
		sequence := index + 1
		payload, err := json.Marshal(streams.AgentEvent{Type: streams.EventTypeMessageChunk, Text: "open fragment"})
		if err != nil {
			t.Fatalf("marshal open event %d: %v", sequence, err)
		}
		event := &models.AgentDeliveryEvent{
			SessionID: sessionID, IncarnationID: "incarnation-" + sessionID, HarnessGeneration: 1,
			StreamID: streamID, Sequence: int64(sequence), EventType: streams.EventTypeMessageChunk,
			Payload: payload, ReceivedAt: baseTime,
		}
		if inserted, err := repo.ReceiveAgentDeliveryEvent(ctx, event, int64(sequence)); err != nil || !inserted {
			t.Fatalf("receive open event %d = %v, err %v", sequence, inserted, err)
		}
		events = append(events, event)
	}
	effects := make([]*models.AgentDeliveryEffect, 0, len(events))
	for _, event := range events {
		effects = append(effects, completedRetentionEventEffect(event))
	}
	if _, err := repo.ProjectCanonicalAgentDeliveryEvents(ctx, events, effects); err != nil {
		t.Fatalf("project open events: %v", err)
	}
	return repo, events, func(delta time.Duration) { currentTime = currentTime.Add(delta) }
}
