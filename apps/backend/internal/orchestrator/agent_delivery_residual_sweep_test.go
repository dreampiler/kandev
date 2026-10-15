package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// TestResidualTerminalSweepNoOp proves Task 02: a block_resolved terminal
// settlement whose workflow receipt is missing gains a guarded no-op receipt
// through the sweep and promotes to completed within the same reconcile.
func TestResidualTerminalSweepNoOp(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-residual-sweep", "session-residual-sweep", "")
	session, err := repo.GetTaskSession(ctx, "session-residual-sweep")
	if err != nil {
		t.Fatal(err)
	}
	incarnationID := session.QueueIncarnationID
	if incarnationID == "" {
		incarnationID = session.ID
	}
	if _, err := repo.PrepareAgentDeliverySubmission(ctx, &models.AgentDeliverySubmission{
		ID: "prompt:residual-sweep", SessionID: session.ID, IncarnationID: incarnationID,
		HarnessGeneration: 1, OwnerGeneration: 1, DispatchAttemptID: "dispatch-residual-sweep",
		PayloadHash: "hash-residual-sweep", Payload: []byte("prompt"),
		State: models.DeliverySubmissionDispatching,
	}); err != nil {
		t.Fatalf("prepare residual submission: %v", err)
	}
	event := persistedTerminalEvent(
		session.ID, incarnationID, 1,
		"prompt:residual-sweep", "stream-residual-sweep", "complete",
	)
	projectOrchestratorDeliveryEvent(t, repo, ctx, event)
	service := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})

	if err := service.reconcileAgentDeliverySettlements(ctx, session.ID); err != nil {
		t.Fatalf("settle residual terminal: %v", err)
	}
	terminal, err := repo.GetAgentDeliveryEffect(ctx, "agent_delivery.terminal:stream-residual-sweep:1")
	if err != nil || terminal.State != models.DeliveryEffectCompleted {
		t.Fatalf("residual terminal = %+v, %v; want completed", terminal, err)
	}

	// The reconcile entry point already ran the residual pass, so the guarded
	// no-op for the turn that never reached on_turn_complete is recorded and an
	// explicit sweep has nothing left to do.
	swept, _, err := service.sweepResidualDeliveryNoOp(ctx, "")
	if err != nil || swept != 0 {
		t.Fatalf("explicit sweep after reconcile = %d, %v; want 0", swept, err)
	}
	submission, err := repo.GetAgentDeliverySubmissionMetadata(ctx, "prompt:residual-sweep")
	if err != nil || submission.FullySettledAt == nil {
		t.Fatalf("residual submission after sweep = %+v, %v; want fully settled", submission, err)
	}
}

// TestSettleDeadOwnerDeliverySubmissionsPass proves the production pass wired
// into reconcileAgentDeliverySettlements settles a dead owner's active
// submission through the existing terminal settlement path.
func TestSettleDeadOwnerDeliverySubmissionsPass(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "task-dead-pass", "session-dead-pass", "")
	session, err := repo.GetTaskSession(ctx, "session-dead-pass")
	if err != nil {
		t.Fatal(err)
	}
	incarnationID := session.QueueIncarnationID
	if incarnationID == "" {
		incarnationID = session.ID
	}
	if _, err := repo.PrepareAgentDeliverySubmission(ctx, &models.AgentDeliverySubmission{
		ID: "prompt:dead-pass", SessionID: session.ID, IncarnationID: incarnationID,
		HarnessGeneration: 1, OwnerGeneration: 1, DispatchAttemptID: "dispatch-dead-pass",
		PayloadHash: "hash-dead-pass", Payload: []byte("prompt"),
		State: models.DeliverySubmissionDispatching,
	}); err != nil {
		t.Fatalf("prepare dead-owner submission: %v", err)
	}
	event := persistedTerminalEvent(
		session.ID, incarnationID, 1, "prompt:dead-pass", "stream-dead-pass", "complete",
	)
	projectOrchestratorDeliveryEvent(t, repo, ctx, event)
	if err := repo.UpdateTaskSessionState(ctx, session.ID, models.TaskSessionStateCancelled, ""); err != nil {
		t.Fatalf("cancel owner session: %v", err)
	}
	terminalAt := time.Now().UTC().Add(-4 * time.Hour)
	if _, err := repo.DB().Exec(
		`UPDATE task_sessions SET updated_at = ?, completed_at = ? WHERE id = ?`,
		terminalAt, terminalAt, session.ID,
	); err != nil {
		t.Fatalf("backdate owner terminal time: %v", err)
	}

	service := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	settled, err := service.settleDeadOwnerDeliverySubmissions(ctx, session.ID)
	if err != nil || settled != 1 {
		t.Fatalf("dead-owner settlement pass = %d, %v; want 1", settled, err)
	}
	submission, err := repo.GetAgentDeliverySubmissionMetadata(ctx, "prompt:dead-pass")
	if err != nil || submission.State != models.DeliverySubmissionCompleted {
		t.Fatalf("dead-owner submission = %+v, %v; want completed", submission, err)
	}
}
