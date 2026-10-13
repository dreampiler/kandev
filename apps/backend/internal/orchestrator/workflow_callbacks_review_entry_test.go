package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/review"
	"github.com/kandev/kandev/internal/steptelemetry"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// These tests drive run_code_review through the production step-entry path —
// a committed step change fires Repository.dispatchStepEntry, which runs
// Engine.DispatchStepEntry — instead of handing the callback a hand-built
// ActionInput. That path carries no session in MachineState, which is why the
// workflow-step review used to fail closed on every real entry: the callback
// read the writer from in.State.SessionID, and that is always empty there.

// reviewEntryWorkflowJSON is a two-step Work/Review workflow whose Review step
// starts a native review on entry, mirroring the AIWS verification step.
const reviewEntryWorkflowJSON = `{
  "version": 1,
  "type": "kandev_workflow",
  "workflows": [
    {
      "name": "ReviewEntry",
      "steps": [
        {
          "name": "Work",
          "position": 0,
          "color": "bg-blue-500",
          "events": {
            "on_turn_complete": [{"type": "move_to_step", "config": {"step_position": 1}}]
          },
          "is_start_step": true,
          "allow_manual_move": true
        },
        {
          "name": "Review",
          "position": 1,
          "color": "bg-yellow-500",
          "events": {
            "on_enter": [{"type": "run_code_review", "config": {"agent_profile_id": "reviewer-dynamic"}}]
          },
          "allow_manual_move": true
        }
      ]
    }
  ]
}`

// recordingReviewRunner records launches; safe for the concurrent on_enter
// goroutines the transition path starts.
type recordingReviewRunner struct {
	mu       sync.Mutex
	requests []review.RunRequest
}

func (r *recordingReviewRunner) Launch(_ context.Context, req review.RunRequest) (*models.TaskReviewRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return &models.TaskReviewRun{ID: "run-1", TaskID: req.TaskID, Status: models.ReviewRunPending}, nil
}

func (r *recordingReviewRunner) launches() []review.RunRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]review.RunRequest(nil), r.requests...)
}

type reviewEntryFixture struct {
	svc      *Service
	repo     *sqliterepo.Repository
	runner   *recordingReviewRunner
	nameToID map[string]string
}

// newReviewEntryFixture seeds task t1 at Work with session s-writer and wires
// the step-entry dispatcher the same way backendapp does.
func newReviewEntryFixture(t *testing.T) *reviewEntryFixture {
	t.Helper()
	sg, nameToID := buildWorkflowFromJSON(t, reviewEntryWorkflowJSON)
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s-writer", nameToID["Work"])
	setSessionExecID(t, repo, "s-writer", "exec-writer")

	agentMgr := &mockAgentManager{repoForExecutionLookup: repo, isAgentRunning: true}
	svc := createEngineService(t, repo, sg, agentMgr)
	runner := &recordingReviewRunner{}
	// SetReviewRunner rebuilds the engine with the run_code_review callback, so
	// it must precede capturing the engine for the dispatcher.
	svc.SetReviewRunner(runner)
	repo.SetStepEntryDispatcher(&testStepEntryDispatcher{eng: svc.WorkflowEngine()})
	seedMockTaskState(svc.taskRepo.(*mockTaskRepo), "t1", v1.TaskStateInProgress)

	return &reviewEntryFixture{svc: svc, repo: repo, runner: runner, nameToID: nameToID}
}

// setSessionIdentity stores a session's logical and concrete profiles, the
// shape a dynamic-profile session has after its route chose a candidate.
func setSessionIdentity(t *testing.T, repo *sqliterepo.Repository, sessionID, logicalProfileID, executionProfileID string) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil {
		t.Fatalf("load session %s: session=%v err=%v", sessionID, session, err)
	}
	session.AgentProfileID = logicalProfileID
	session.ExecutionProfileID = executionProfileID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("update session %s identity: %v", sessionID, err)
	}
}

// addTaskSession adds another session to an existing task.
func addTaskSession(t *testing.T, repo *sqliterepo.Repository, taskID, sessionID, logicalProfileID, executionProfileID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := repo.CreateTaskSession(context.Background(), &models.TaskSession{
		ID: sessionID, TaskID: taskID, State: models.TaskSessionStateWaitingForInput,
		AgentProfileID: logicalProfileID, ExecutionProfileID: executionProfileID,
		StartedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create session %s: %v", sessionID, err)
	}
}

// moveTaskTo commits a step change through the repository writer with the given
// ledger attribution, which is what fires step-entry dispatch in production.
func (f *reviewEntryFixture) moveTaskTo(t *testing.T, attribution steptelemetry.Attribution, stepName string) {
	t.Helper()
	ctx := steptelemetry.WithAttribution(context.Background(), attribution)
	task, err := f.repo.GetTask(ctx, "t1")
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	task.WorkflowStepID = f.nameToID[stepName]
	if err := f.repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("move task to %s: %v", stepName, err)
	}
}

func (f *reviewEntryFixture) singleLaunch(t *testing.T) review.RunRequest {
	t.Helper()
	launches := f.runner.launches()
	if len(launches) != 1 {
		t.Fatalf("review launches = %d, want exactly one for one step entry", len(launches))
	}
	got := launches[0]
	if !got.OriginRequired {
		t.Fatal("a workflow-step review must require origin exclusion")
	}
	if got.EntryID == "" {
		t.Fatal("a step-entry review must carry the ledger entry identity")
	}
	if got.WorkflowStepID != f.nameToID["Review"] {
		t.Fatalf("workflow step = %q, want the Review step", got.WorkflowStepID)
	}
	return got
}

func assertOrigin(t *testing.T, got *review.OriginIdentity, wantLogical, wantExecution string) {
	t.Helper()
	if got == nil {
		t.Fatalf("origin = nil, want {%s %s}: the review would fail closed with review_agent_unavailable", wantLogical, wantExecution)
	}
	if got.ProfileID != wantLogical || got.ExecutionProfileID != wantExecution {
		t.Fatalf("origin = %+v, want logical %q / concrete %q", *got, wantLogical, wantExecution)
	}
}

// TestRunCodeReviewStepEntry_TurnCompleteUsesTheCompletingSession covers the
// production failure (4/4 entries since 10-13 00:21): an on_turn_complete
// transition into the review step must hand the resolver the identity of the
// session whose turn produced the change — even when it is not the task's
// primary — including the concrete candidate a dynamic profile actually ran.
func TestRunCodeReviewStepEntry_TurnCompleteUsesTheCompletingSession(t *testing.T) {
	ctx := context.Background()
	f := newReviewEntryFixture(t)
	setSessionIdentity(t, f.repo, "s-writer", "writer-dynamic", "writer-concrete")
	addTaskSession(t, f.repo, "t1", "s-primary", "other-logical", "other-concrete")
	if err := f.repo.SetSessionPrimary(ctx, "s-primary"); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	setSessionState(t, ctx, f.repo, "s-writer", models.TaskSessionStateRunning)

	onEnterDone := make(chan struct{})
	f.svc.onProcessOnEnterComplete = func() { close(onEnterDone) }
	session, err := f.repo.GetTaskSession(ctx, "s-writer")
	if err != nil {
		t.Fatalf("load writer session: %v", err)
	}
	if !f.svc.processOnTurnCompleteViaEngine(ctx, "t1", session) {
		t.Fatal("expected on_turn_complete to move the task Work -> Review")
	}
	select {
	case <-onEnterDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for on_enter processing")
	}

	got := f.singleLaunch(t)
	assertOrigin(t, got.Origin, "writer-dynamic", "writer-concrete")
	if got.AgentProfileID != "reviewer-dynamic" {
		t.Fatalf("reviewer profile = %q, want the step's declared profile", got.AgentProfileID)
	}
}

// TestRunCodeReviewStepEntry_ManualMoveUsesThePrimarySession covers a human
// moving the card into the review step: no session initiated the move, so the
// writer is the task's primary session.
func TestRunCodeReviewStepEntry_ManualMoveUsesThePrimarySession(t *testing.T) {
	f := newReviewEntryFixture(t)
	setSessionIdentity(t, f.repo, "s-writer", "writer-dynamic", "writer-concrete")
	if err := f.repo.SetSessionPrimary(context.Background(), "s-writer"); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	addTaskSession(t, f.repo, "t1", "s-side", "side-logical", "side-concrete")

	f.moveTaskTo(t, steptelemetry.Attribution{
		Trigger: steptelemetry.TriggerManualMove, ActorKind: steptelemetry.ActorHuman, ActorID: "default-user",
	}, "Review")

	assertOrigin(t, f.singleLaunch(t).Origin, "writer-dynamic", "writer-concrete")
}

// TestRunCodeReviewStepEntry_MoveByAnotherTasksSessionUsesThePrimary covers a
// coordinator session from a different task moving the card (an mcp_move the
// ledger attributes to the coordinator): the coordinator did not write this
// task's change, so the writer is still this task's primary session.
func TestRunCodeReviewStepEntry_MoveByAnotherTasksSessionUsesThePrimary(t *testing.T) {
	ctx := context.Background()
	f := newReviewEntryFixture(t)
	setSessionIdentity(t, f.repo, "s-writer", "writer-dynamic", "writer-concrete")
	if err := f.repo.SetSessionPrimary(ctx, "s-writer"); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	now := time.Now().UTC()
	if err := f.repo.CreateTask(ctx, &models.Task{
		ID: "t-coordinator", WorkspaceID: "ws1", WorkflowID: "wf1", WorkflowStepID: f.nameToID["Work"],
		Title: "Coordinator", State: v1.TaskStateInProgress, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create coordinator task: %v", err)
	}
	addTaskSession(t, f.repo, "t-coordinator", "s-coordinator", "coordinator-logical", "coordinator-concrete")

	f.moveTaskTo(t, steptelemetry.Attribution{
		Trigger: steptelemetry.TriggerMCPMove, ActorKind: steptelemetry.ActorAgent,
		ActorID: "s-coordinator", SessionID: "s-coordinator",
	}, "Review")

	assertOrigin(t, f.singleLaunch(t).Origin, "writer-dynamic", "writer-concrete")
}

// TestRunCodeReviewStepEntry_UnidentifiableWriterFailsClosed keeps the
// fail-closed contract (AC-AGENTS-NATIVE-CODE-REVIEW-002.4): when neither the
// initiating session nor a primary session identifies the writer, the review
// launches with no origin and OriginRequired, so resolution refuses to pick a
// reviewer it cannot prove distinct — and the move itself still succeeds.
func TestRunCodeReviewStepEntry_UnidentifiableWriterFailsClosed(t *testing.T) {
	t.Run("no primary and no initiating session", func(t *testing.T) {
		f := newReviewEntryFixture(t)
		setSessionIdentity(t, f.repo, "s-writer", "writer-dynamic", "writer-concrete")

		f.moveTaskTo(t, steptelemetry.Attribution{
			Trigger: steptelemetry.TriggerManualMove, ActorKind: steptelemetry.ActorHuman,
		}, "Review")

		if got := f.singleLaunch(t).Origin; got != nil {
			t.Fatalf("origin = %+v, want nil when no session identifies the writer", *got)
		}
	})
	t.Run("primary session without any profile identity", func(t *testing.T) {
		f := newReviewEntryFixture(t)
		if err := f.repo.SetSessionPrimary(context.Background(), "s-writer"); err != nil {
			t.Fatalf("set primary: %v", err)
		}

		f.moveTaskTo(t, steptelemetry.Attribution{
			Trigger: steptelemetry.TriggerManualMove, ActorKind: steptelemetry.ActorHuman,
		}, "Review")

		if got := f.singleLaunch(t).Origin; got != nil {
			t.Fatalf("origin = %+v, want nil for a writer with no profile identity", *got)
		}
	})
}
