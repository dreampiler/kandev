package orchestrator

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// TestResidualTerminalSweepNoOp proves Task 02: a block_resolved terminal
// settlement whose workflow receipt is missing gains a guarded no-op receipt
// through the sweep, then promotes to completed on the next reconcile.
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
	if err != nil || terminal.State != models.DeliveryEffectBlockResolved {
		t.Fatalf("residual terminal = %+v, %v; want block_resolved before the sweep", terminal, err)
	}

	swept, err := service.sweepResidualDeliveryNoOp(ctx)
	if err != nil || swept != 1 {
		t.Fatalf("residual sweep = %d, %v; want 1", swept, err)
	}
	if err := service.reconcileAgentDeliverySettlements(ctx, session.ID); err != nil {
		t.Fatalf("reconcile after residual sweep: %v", err)
	}
	submission, err := repo.GetAgentDeliverySubmissionMetadata(ctx, "prompt:residual-sweep")
	if err != nil || submission.FullySettledAt == nil {
		t.Fatalf("residual submission after sweep = %+v, %v; want fully settled", submission, err)
	}

	// Second sweep is a no-op: nothing left to record.
	swept, err = service.sweepResidualDeliveryNoOp(ctx)
	if err != nil || swept != 0 {
		t.Fatalf("second residual sweep = %d, %v; want 0", swept, err)
	}
}
