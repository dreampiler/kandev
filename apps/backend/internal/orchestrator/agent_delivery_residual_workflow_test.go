package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// residualWorkflowFixture seeds a workflow-bound session with one projected
// block_resolved terminal whose turn carries the given step-entry stamp, so a
// companion test can drive the residual sweep without the live engine.
type residualWorkflowFixture struct {
	ctx     context.Context
	repo    *sqliterepo.Repository
	service *Service
	steps   *mockStepGetter
	taskID  string
	session *models.TaskSession
}

func newResidualWorkflowFixture(t *testing.T, suffix, currentStep, turnStamp string) *residualWorkflowFixture {
	t.Helper()
	ctx := context.Background()
	taskID, sessionID := "residual-wf-task-"+suffix, "residual-wf-session-"+suffix
	repo := setupTestRepo(t)
	seedSession(t, repo, taskID, sessionID, currentStep)
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("load residual workflow session: %v", err)
	}
	incarnationID := session.QueueIncarnationID
	if incarnationID == "" {
		incarnationID = session.ID
	}
	submissionID := "prompt:" + suffix
	if _, err := repo.PrepareAgentDeliverySubmission(ctx, &models.AgentDeliverySubmission{
		ID: submissionID, SessionID: sessionID, IncarnationID: incarnationID,
		HarnessGeneration: 1, OwnerGeneration: 1, DispatchAttemptID: "dispatch-" + suffix,
		PayloadHash: "payload-" + suffix, Payload: []byte("retained prompt"),
		State: models.DeliverySubmissionDispatching,
	}); err != nil {
		t.Fatalf("prepare residual workflow submission: %v", err)
	}
	streamID := "residual-wf-stream-" + suffix
	event := persistedTerminalEvent(sessionID, incarnationID, 1, submissionID, streamID, "complete")
	if err := repo.CreateTurn(ctx, &models.Turn{
		ID: eventTurnID(event), TaskSessionID: sessionID, TaskID: taskID,
		StartedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Metadata: map[string]interface{}{models.TurnMetaKeyWorkflowStepIDAtStart: turnStamp},
	}); err != nil {
		t.Fatalf("create stamped turn: %v", err)
	}
	projectOrchestratorDeliveryEvent(t, repo, ctx, event)
	stepGetter := newMockStepGetter()
	taskRepo := newMockTaskRepo()
	taskRepo.tasks[taskID] = &v1.Task{ID: taskID, State: v1.TaskStateInProgress}
	service := createEngineServiceWithTaskRepo(t, repo, stepGetter, taskRepo, &mockAgentManager{})
	return &residualWorkflowFixture{
		ctx: ctx, repo: repo, service: service, steps: stepGetter, taskID: taskID, session: session,
	}
}

// installStep registers a step both as the durable workflow policy row
// (requireWorkflowOutcomeSourceTx compares its updated_at) and in the step
// getter the guarded writer reads the policy from.
func (f *residualWorkflowFixture) installStep(t *testing.T, step *wfmodels.WorkflowStep) {
	t.Helper()
	if _, err := f.repo.DB().ExecContext(f.ctx, `
		INSERT INTO workflow_steps (id, workflow_id, name, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, step.ID, step.WorkflowID, step.ID, step.Position,
		step.UpdatedAt, step.UpdatedAt); err != nil {
		t.Fatalf("insert workflow policy step: %v", err)
	}
	f.steps.steps[step.ID] = step
}

func (f *residualWorkflowFixture) submissionID(suffix string) string { return "prompt:" + suffix }

func (f *residualWorkflowFixture) assertReceiptOutcome(t *testing.T, suffix, want string) {
	t.Helper()
	stored, err := f.repo.GetAgentDeliveryEffect(f.ctx, "workflow.on_turn_complete:turn-"+f.submissionID(suffix))
	if err != nil || stored == nil || stored.Outcome != want {
		t.Fatalf("residual workflow receipt = %+v, %v; want outcome %q", stored, err, want)
	}
}

func TestResidualWorkflowReplacedTurnRecordsNotRequired(t *testing.T) {
	// The turn is stamped in a step the task has since left, so the step move
	// replaced it and its completion cannot drive an entry.
	fixture := newResidualWorkflowFixture(t, "replaced", "step-new", "step-old")
	stepTime := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	fixture.installStep(t, &wfmodels.WorkflowStep{ID: "step-new", WorkflowID: "wf1", UpdatedAt: stepTime})

	if err := fixture.service.reconcileAgentDeliverySettlements(fixture.ctx, fixture.session.ID); err != nil {
		t.Fatalf("reconcile replaced turn: %v", err)
	}
	fixture.assertReceiptOutcome(t, "replaced", agentDeliveryOutcomeNotRequired)

	if err := fixture.service.reconcileAgentDeliverySettlements(fixture.ctx, fixture.session.ID); err != nil {
		t.Fatalf("reconcile after replaced-turn receipt: %v", err)
	}
	submission, err := fixture.repo.GetAgentDeliverySubmissionMetadata(fixture.ctx, fixture.submissionID("replaced"))
	if err != nil || submission.FullySettledAt == nil {
		t.Fatalf("replaced-turn submission = %+v, %v; want fully settled", submission, err)
	}
}

func TestResidualWorkflowCurrentTurnWithoutAutomaticRecordsNoAutomaticTransition(t *testing.T) {
	// A current-step turn whose step declares no automatic on_turn_complete
	// move deterministically yields no_automatic_transition.
	fixture := newResidualWorkflowFixture(t, "noauto", "step-quiet", "step-quiet")
	stepTime := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	fixture.installStep(t, &wfmodels.WorkflowStep{ID: "step-quiet", WorkflowID: "wf1", UpdatedAt: stepTime})

	if err := fixture.service.reconcileAgentDeliverySettlements(fixture.ctx, fixture.session.ID); err != nil {
		t.Fatalf("reconcile no-automatic turn: %v", err)
	}
	fixture.assertReceiptOutcome(t, "noauto", agentDeliveryOutcomeNoAutomatic)

	if err := fixture.service.reconcileAgentDeliverySettlements(fixture.ctx, fixture.session.ID); err != nil {
		t.Fatalf("reconcile after no-automatic receipt: %v", err)
	}
	submission, err := fixture.repo.GetAgentDeliverySubmissionMetadata(fixture.ctx, fixture.submissionID("noauto"))
	if err != nil || submission.FullySettledAt == nil {
		t.Fatalf("no-automatic submission = %+v, %v; want fully settled", submission, err)
	}
}

func TestResidualWorkflowCurrentTurnWithAutomaticStaysPinned(t *testing.T) {
	// A current-step turn whose step declares an automatic move must stay
	// pinned: the sweep counts it but never fabricates a transition.
	fixture := newResidualWorkflowFixture(t, "pinned", "step-move", "step-move")
	stepTime := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	fixture.installStep(t, &wfmodels.WorkflowStep{
		ID: "step-move", WorkflowID: "wf1", UpdatedAt: stepTime,
		Events: wfmodels.StepEvents{OnTurnComplete: []wfmodels.OnTurnCompleteAction{{Type: wfmodels.OnTurnCompleteMoveToNext}}},
	})

	if err := fixture.service.reconcileAgentDeliverySettlements(fixture.ctx, fixture.session.ID); err != nil {
		t.Fatalf("reconcile pinned turn: %v", err)
	}
	swept, pinned, err := fixture.service.sweepResidualDeliveryNoOp(fixture.ctx, fixture.session.ID)
	if err != nil || swept != 0 || pinned != 1 {
		t.Fatalf("pinned sweep = (%d, %d), %v; want (0, 1)", swept, pinned, err)
	}
	if _, err := fixture.repo.GetAgentDeliveryEffect(fixture.ctx,
		"workflow.on_turn_complete:turn-"+fixture.submissionID("pinned")); err == nil {
		t.Fatal("pinned turn gained a workflow receipt; want none")
	}
	terminal, err := fixture.repo.GetAgentDeliveryEffect(fixture.ctx, "agent_delivery.terminal:residual-wf-stream-pinned:1")
	if err != nil || terminal.State != models.DeliveryEffectBlockResolved {
		t.Fatalf("pinned terminal = %+v, %v; want block_resolved", terminal, err)
	}
	submission, err := fixture.repo.GetAgentDeliverySubmissionMetadata(fixture.ctx, fixture.submissionID("pinned"))
	if err != nil || submission.FullySettledAt != nil {
		t.Fatalf("pinned submission = %+v, %v; want not fully settled", submission, err)
	}
}

func TestResidualWorkflowMissingTurnStaysPinned(t *testing.T) {
	// Without the turn row there is no entry/turn evidence, so the sweep must
	// not fabricate a receipt.
	fixture := newResidualWorkflowFixture(t, "noturn", "step-noturn", "step-noturn")
	stepTime := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	fixture.installStep(t, &wfmodels.WorkflowStep{ID: "step-noturn", WorkflowID: "wf1", UpdatedAt: stepTime})
	if _, err := fixture.repo.DB().ExecContext(fixture.ctx, `DELETE FROM task_session_turns`); err != nil {
		t.Fatalf("remove turn row: %v", err)
	}

	if err := fixture.service.reconcileAgentDeliverySettlements(fixture.ctx, fixture.session.ID); err != nil {
		t.Fatalf("reconcile missing-turn terminal: %v", err)
	}
	if _, err := fixture.repo.GetAgentDeliveryEffect(fixture.ctx,
		"workflow.on_turn_complete:turn-"+fixture.submissionID("noturn")); err == nil {
		t.Fatal("missing-turn terminal gained a workflow receipt; want none")
	}
	terminal, err := fixture.repo.GetAgentDeliveryEffect(fixture.ctx, "agent_delivery.terminal:residual-wf-stream-noturn:1")
	if err != nil || terminal.State != models.DeliveryEffectBlockResolved {
		t.Fatalf("missing-turn terminal = %+v, %v; want block_resolved", terminal, err)
	}
	submission, err := fixture.repo.GetAgentDeliverySubmissionMetadata(fixture.ctx, fixture.submissionID("noturn"))
	if err != nil || submission.FullySettledAt != nil {
		t.Fatalf("missing-turn submission = %+v, %v; want not fully settled", submission, err)
	}
}
