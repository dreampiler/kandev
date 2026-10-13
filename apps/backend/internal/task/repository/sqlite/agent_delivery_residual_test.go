package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/task/models"
)

// TestResidualDeadOwnerSettlement proves Task 01: a dispatching submission
// whose owner session is CANCELLED past the payload grace, with no open
// recovery block and no queued/dispatch/send-now signals, settles through the
// existing terminal path once its projected terminal row exists.
func TestResidualDeadOwnerSettlement(t *testing.T) {
	for _, engine := range []string{"SQLite", "Postgres"} {
		t.Run(engine+"/dead_owner_settles", func(t *testing.T) {
			repo, event, _ := seedResidualDeadOwner(t, engine, "dead-owner")
			ctx := context.Background()
			settled, err := repo.SettleDeadOwnerAgentDeliverySubmission(ctx, event.SubmissionID)
			if err != nil || !settled {
				t.Fatalf("dead owner settlement = %v, %v; want settled", settled, err)
			}
			submission, err := repo.GetAgentDeliverySubmissionMetadata(ctx, event.SubmissionID)
			if err != nil || submission.State != models.DeliverySubmissionCompleted {
				t.Fatalf("dead owner submission not terminal: %+v, %v", submission, err)
			}
			terminal, err := repo.GetAgentDeliveryEffect(ctx, "agent_delivery.terminal:"+event.StreamID+":1")
			if err != nil || terminal.State != models.DeliveryEffectBlockResolved {
				t.Fatalf("dead owner terminal effect = %+v, %v; want block_resolved", terminal, err)
			}
		})

		t.Run(engine+"/live_owner_untouched", func(t *testing.T) {
			repo, event, _ := seedResidualDeadOwner(t, engine, "live-owner")
			ctx := context.Background()
			if err := repo.UpdateTaskSessionState(ctx, event.SessionID, models.TaskSessionStateRunning, ""); err != nil {
				t.Fatalf("revive owner session: %v", err)
			}
			settled, err := repo.SettleDeadOwnerAgentDeliverySubmission(ctx, event.SubmissionID)
			if err != nil || settled {
				t.Fatalf("live owner settlement = %v, %v; want untouched", settled, err)
			}
		})

		t.Run(engine+"/open_block_untouched", func(t *testing.T) {
			repo, event, _ := seedResidualDeadOwner(t, engine, "open-block")
			ctx := context.Background()
			settled, err := repo.SettleDeadOwnerAgentDeliverySubmission(ctx, event.SubmissionID)
			if err != nil || settled {
				t.Fatalf("open block settlement = %v, %v; want untouched", settled, err)
			}
		})

		t.Run(engine+"/no_terminal_row_untouched", func(t *testing.T) {
			repo := newRetentionTestRepo(t, engine)
			ctx := context.Background()
			now := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
			repo.clockNow = func() time.Time { return now }
			taskID, sessionID := "residual-no-term-task", "residual-no-term-session"
			if err := repo.CreateTask(ctx, &models.Task{
				ID: taskID, WorkspaceID: retentionTestWorkspaceID(t, repo), Title: "Residual",
			}); err != nil {
				t.Fatalf("create task: %v", err)
			}
			if err := repo.CreateTaskSession(ctx, &models.TaskSession{
				ID: sessionID, TaskID: taskID, QueueIncarnationID: "inc-" + sessionID,
				State:     models.TaskSessionStateCancelled,
				StartedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour),
			}); err != nil {
				t.Fatalf("create residual session: %v", err)
			}
			if _, err := repo.PrepareAgentDeliverySubmission(ctx, &models.AgentDeliverySubmission{
				ID: "residual-no-term-sub", SessionID: sessionID, IncarnationID: "inc-" + sessionID,
				HarnessGeneration: 1, OwnerGeneration: 1, DispatchAttemptID: "dispatch-no-term",
				PayloadHash: "hash-no-term", Payload: []byte("prompt"),
				State: models.DeliverySubmissionDispatching, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("prepare submission: %v", err)
			}
			settled, err := repo.SettleDeadOwnerAgentDeliverySubmission(ctx, "residual-no-term-sub")
			if err != nil || settled {
				t.Fatalf("terminal-less settlement = %v, %v; want untouched", settled, err)
			}
		})
	}
}

func seedResidualDeadOwner(t *testing.T, engine, suffix string) (*Repository, models.AgentDeliveryEvent, func(time.Duration)) {
	t.Helper()
	repo := newRetentionTestRepo(t, engine)
	ctx := context.Background()
	now := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
	currentTime := now
	repo.clockNow = func() time.Time { return currentTime }
	taskID, sessionID := "residual-task-"+suffix, "residual-session-"+suffix
	incarnationID, submissionID := "residual-inc-"+suffix, "residual-sub-"+suffix
	if err := repo.CreateTask(ctx, &models.Task{
		ID: taskID, WorkspaceID: retentionTestWorkspaceID(t, repo), Title: "Residual",
	}); err != nil {
		t.Fatalf("create residual task: %v", err)
	}
	if err := repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: sessionID, TaskID: taskID, QueueIncarnationID: incarnationID,
		State:     models.TaskSessionStateCancelled,
		StartedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour),
	}); err != nil {
		t.Fatalf("create residual session: %v", err)
	}
	if _, err := repo.PrepareAgentDeliverySubmission(ctx, &models.AgentDeliverySubmission{
		ID: submissionID, SessionID: sessionID, IncarnationID: incarnationID,
		HarnessGeneration: 1, OwnerGeneration: 1, DispatchAttemptID: "dispatch-" + suffix,
		PayloadHash: "hash-" + suffix, Payload: []byte("prompt"),
		State: models.DeliverySubmissionDispatching, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("prepare residual submission: %v", err)
	}
	payload, err := json.Marshal(streams.AgentEvent{Type: streams.EventTypeComplete, TurnID: "residual-turn-" + suffix})
	if err != nil {
		t.Fatalf("marshal terminal event: %v", err)
	}
	event := models.AgentDeliveryEvent{
		SessionID: sessionID, IncarnationID: incarnationID, HarnessGeneration: 1,
		StreamID: "residual-stream-" + suffix, Sequence: 1, SubmissionID: submissionID,
		EventType: "complete", Payload: payload, Terminal: true, ReceivedAt: now,
	}
	if inserted, err := repo.ReceiveAgentDeliveryEvent(ctx, &event, 1); err != nil || !inserted {
		t.Fatalf("receive residual terminal = %v, %v", inserted, err)
	}
	if _, err := repo.ProjectAgentDeliveryEvent(ctx, &event, nil); err != nil {
		t.Fatalf("project residual terminal: %v", err)
	}
	if suffix == "open-block" {
		if err := repo.UpsertSessionRecoveryBlock(ctx, &models.SessionRecoveryBlock{
			ID: "residual-block-" + suffix, SessionID: sessionID, IncarnationID: incarnationID,
			ExpectedGeneration: 1, Reason: "unknown_prompt_outcome", State: models.RecoveryBlockOpen,
			ConsumerReference: "agent_delivery", DeliverySubmissionID: submissionID,
		}); err != nil {
			t.Fatalf("create residual block: %v", err)
		}
	}
	return repo, event, func(delta time.Duration) { currentTime = currentTime.Add(delta) }
}
