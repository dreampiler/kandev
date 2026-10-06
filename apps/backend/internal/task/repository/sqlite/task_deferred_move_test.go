package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	workflowmove "github.com/kandev/kandev/internal/workflow/move"
)

func TestUpdateTaskWithWorkflowStepAdmissionForDeferredMovePersistsQueuedEntryOptionsAtomically(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedWorkspace(t, repo, "workspace-deferred-options")
	if err := repo.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-deferred-options", WorkspaceID: "workspace-deferred-options", Name: "Workflow"}); err != nil {
		t.Fatal(err)
	}
	seedCASWorkflowStep(t, repo, "workflow-deferred-options", "step-source-options", 0)
	seedCASWorkflowStep(t, repo, "workflow-deferred-options", "step-target-options", 1)

	occupant := &models.Task{
		ID: "task-deferred-options-occupant", WorkspaceID: "workspace-deferred-options", WorkflowID: "workflow-deferred-options",
		WorkflowStepID: "step-target-options", Title: "Occupant", WIPAdmitted: true,
	}
	if err := repo.CreateTask(ctx, occupant); err != nil {
		t.Fatal(err)
	}
	candidate := &models.Task{
		ID: "task-deferred-options", WorkspaceID: "workspace-deferred-options", WorkflowID: "workflow-deferred-options",
		WorkflowStepID: "step-source-options", Title: "Deferred candidate", WIPAdmitted: true,
	}
	if err := repo.CreateTask(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	session := &models.TaskSession{
		ID: "session-deferred-options", TaskID: candidate.ID, QueueIncarnationID: "incarnation-options",
		State: models.TaskSessionStateWaitingForInput, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.CreateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	queueRepo, err := messagequeue.NewSQLiteRepository(repo.db, repo.ro)
	if err != nil {
		t.Fatal(err)
	}
	move := messagequeue.PendingMove{
		MoveID: "move-deferred-options", SessionIncarnationID: session.QueueIncarnationID,
		TaskID: candidate.ID, WorkflowID: candidate.WorkflowID, WorkflowStepID: "step-target-options",
		QueuedAt: time.Now().UTC(), EntryOptions: &workflowmove.EntryOptions{
			ResetContext: true, Instructions: "focus on the auth bug", SkipStepPrompt: true,
		},
	}
	if err := queueRepo.SetPendingMove(ctx, session.ID, &move); err != nil {
		t.Fatal(err)
	}

	admitted, applied, err := repo.UpdateTaskWithWorkflowStepAdmissionForDeferredMove(
		ctx, candidate, "step-source-options", "step-target-options", 1,
		messagequeue.PendingMoveRecord{SessionID: session.ID, Move: move},
	)
	if err != nil {
		t.Fatal(err)
	}
	if admitted || !applied {
		t.Fatalf("admitted=%t applied=%t, want queued applied move", admitted, applied)
	}

	stored, err := repo.GetTask(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorkflowStepID != "step-target-options" || stored.QueuedForStepID != "step-target-options" || stored.WIPAdmitted {
		t.Fatalf("candidate placement = %+v, want queued at target", stored)
	}
	marker, ok := stored.Metadata[models.MetaKeyWorkflowMovePending].(map[string]interface{})
	if !ok {
		t.Fatalf("queued deferred move must retain a workflow move marker, metadata=%+v", stored.Metadata)
	}
	if marker["from_step_id"] != "step-source-options" || marker["move_id"] != move.MoveID {
		t.Fatalf("marker identity = %+v, want source and move ID", marker)
	}
	encoded, ok := marker["options"].(string)
	if !ok || encoded == "" {
		t.Fatalf("marker options = %v, want encoded entry options", marker["options"])
	}
	decoded, err := workflowmove.DecodeEntryOptionsJSON([]byte(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if *decoded != *move.EntryOptions {
		t.Fatalf("decoded marker options = %+v, want %+v", decoded, move.EntryOptions)
	}
	if pending, err := queueRepo.GetPendingMove(ctx, session.ID); err != nil {
		t.Fatal(err)
	} else if pending != nil {
		t.Fatalf("deferred move row must be consumed atomically, got %+v", pending)
	}
}

func TestExactWIPMoveDoesNotPromoteAfterManagementTransfer(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedWorkspace(t, repo, "workspace-claim-promotion")
	if err := repo.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-claim-promotion", WorkspaceID: "workspace-claim-promotion", Name: "Workflow"}); err != nil {
		t.Fatal(err)
	}
	seedCASWorkflowStep(t, repo, "workflow-claim-promotion", "step-claim-source", 0)
	seedCASWorkflowStep(t, repo, "workflow-claim-promotion", "step-claim-target", 1)
	occupant := &models.Task{
		ID: "task-claim-promotion-occupant", WorkspaceID: "workspace-claim-promotion", WorkflowID: "workflow-claim-promotion",
		WorkflowStepID: "step-claim-target", Title: "Occupant", WIPAdmitted: true,
	}
	if err := repo.CreateTask(ctx, occupant); err != nil {
		t.Fatal(err)
	}
	candidate := &models.Task{
		ID: "task-claim-promotion", WorkspaceID: "workspace-claim-promotion", WorkflowID: "workflow-claim-promotion",
		WorkflowStepID: "step-claim-source", Title: "Candidate", WIPAdmitted: true,
	}
	if err := repo.CreateTask(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	version := candidate.UpdatedAt.UTC().Format(time.RFC3339Nano)
	claim, err := repo.ChangeTaskManagementClaim(ctx, models.TaskManagementClaimChange{
		Action: models.TaskManagementClaimAcquire, TaskID: candidate.ID, WorkspaceID: candidate.WorkspaceID,
		ExpectedTaskResourceVersion: version, InstallationID: "old-manager", InstanceKey: "instance-1", ActorID: "plugin:old-manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	admitted, replayed, err := repo.UpdateTaskWithWorkflowStepAdmissionExact(
		ctx, candidate, "step-claim-source", "step-claim-target", 1, nil, false,
		candidate.WorkflowID, candidate.WorkspaceID, version, "move-claim-1", "digest-claim-1",
		models.TaskManagementClaimFence{InstallationID: "old-manager", InstanceKey: "instance-1", Generation: claim.Generation},
	)
	if err != nil {
		t.Fatal(err)
	}
	if admitted || replayed {
		t.Fatalf("move admitted=%t replayed=%t, want deferred first application", admitted, replayed)
	}
	queued, err := repo.GetTask(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := queued.Metadata[models.MetaKeyTaskManagementDeferredFence]; !ok {
		t.Fatalf("deferred task lacks manager fence: %+v", queued.Metadata)
	}
	if _, leaked := models.PublicTaskMetadata(queued.Metadata)[models.MetaKeyTaskManagementDeferredFence]; leaked {
		t.Fatal("internal manager fence leaked in public task metadata")
	}
	_, err = repo.ChangeTaskManagementClaim(ctx, models.TaskManagementClaimChange{
		Action: models.TaskManagementClaimTransfer, TaskID: queued.ID, WorkspaceID: queued.WorkspaceID,
		ExpectedTaskResourceVersion:  queued.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ExpectedClaimResourceVersion: claim.ResourceVersion,
		OwnerKind:                    "human", OwnerActorID: "user-1", ActorID: "user-1", Reason: "take over queued move",
	})
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := repo.PromoteQueuedTaskIfWorkflowStepHasCapacity(ctx, queued, "step-claim-target", "step-claim-target", 0)
	if err != nil {
		t.Fatal(err)
	}
	if promoted {
		t.Fatal("old manager's queued move promoted after ownership transfer")
	}
	stillQueued, err := repo.GetTask(ctx, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillQueued.WIPAdmitted || stillQueued.QueuedForStepID != "step-claim-target" {
		t.Fatalf("stale deferred move changed task placement: %+v", stillQueued)
	}
}

func TestUpdateTaskWithWorkflowStepAdmissionForDeferredMoveRejectsReplacementSession(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedWorkspace(t, repo, "workspace-deferred")
	if err := repo.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-deferred", WorkspaceID: "workspace-deferred", Name: "Workflow"}); err != nil {
		t.Fatal(err)
	}
	seedCASWorkflowStep(t, repo, "workflow-deferred", "step-source", 0)
	seedCASWorkflowStep(t, repo, "workflow-deferred", "step-target", 1)
	task := &models.Task{
		ID: "task-deferred", WorkspaceID: "workspace-deferred", WorkflowID: "workflow-deferred",
		WorkflowStepID: "step-source", Title: "Deferred candidate", WIPAdmitted: true,
	}
	if err := repo.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	session := &models.TaskSession{
		ID: "session-deferred", TaskID: task.ID, QueueIncarnationID: "incarnation-original",
		State: models.TaskSessionStateWaitingForInput, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.CreateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	queueRepo, err := messagequeue.NewSQLiteRepository(repo.db, repo.ro)
	if err != nil {
		t.Fatal(err)
	}
	move := messagequeue.PendingMove{
		MoveID: "move-deferred", SessionIncarnationID: session.QueueIncarnationID,
		TaskID: task.ID, WorkflowID: task.WorkflowID, WorkflowStepID: "step-target",
		QueuedAt: time.Now().UTC(),
	}
	if err := queueRepo.SetPendingMove(ctx, session.ID, &move); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, repo.db.Rebind(
		`UPDATE task_sessions SET queue_incarnation_id = ? WHERE id = ?`),
		"incarnation-replacement", session.ID,
	); err != nil {
		t.Fatal(err)
	}

	_, applied, err := repo.UpdateTaskWithWorkflowStepAdmissionForDeferredMove(
		ctx, task, "step-source", "step-target", 0,
		messagequeue.PendingMoveRecord{SessionID: session.ID, Move: move},
	)
	if !errors.Is(err, messagequeue.ErrSessionIdentityMismatch) {
		t.Fatalf("expected session identity mismatch, got %v", err)
	}
	if applied {
		t.Fatal("replacement session must not apply the deferred move")
	}
	stored, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorkflowStepID != "step-source" {
		t.Fatalf("task moved to %q despite replacement session", stored.WorkflowStepID)
	}
	pending, err := queueRepo.GetPendingMove(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.MoveID != move.MoveID {
		t.Fatalf("pending move was not preserved: %+v", pending)
	}
}

// A deferred move records the source step and its latest transition-ledger id
// when armed. A later move that leaves and returns to the same source step
// (A→B→A) advances the ledger id, so replay must drop the deferred move even
// though the current step equals the recorded source. Without the ledger
// comparison the atomic admission only sees the matching step id and applies
// the stale move.
func TestUpdateTaskWithWorkflowStepAdmissionForDeferredMoveDropsMoveSupersededByLeaveAndReturn(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedWorkspace(t, repo, "workspace-deferred-abba")
	if err := repo.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-deferred-abba", WorkspaceID: "workspace-deferred-abba", Name: "Workflow"}); err != nil {
		t.Fatal(err)
	}
	seedCASWorkflowStep(t, repo, "workflow-deferred-abba", "step-a", 0)
	seedCASWorkflowStep(t, repo, "workflow-deferred-abba", "step-b", 1)
	seedCASWorkflowStep(t, repo, "workflow-deferred-abba", "step-target", 2)

	task := &models.Task{
		ID: "task-deferred-abba", WorkspaceID: "workspace-deferred-abba", WorkflowID: "workflow-deferred-abba",
		WorkflowStepID: "step-a", Title: "Deferred candidate", WIPAdmitted: true,
	}
	if err := repo.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	session := &models.TaskSession{
		ID: "session-deferred-abba", TaskID: task.ID, QueueIncarnationID: "incarnation-abba",
		State: models.TaskSessionStateWaitingForInput, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.CreateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	armTransitionID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	queueRepo, err := messagequeue.NewSQLiteRepository(repo.db, repo.ro)
	if err != nil {
		t.Fatal(err)
	}
	move := messagequeue.PendingMove{
		MoveID: "move-deferred-abba", SessionIncarnationID: session.QueueIncarnationID,
		TaskID: task.ID, WorkflowID: task.WorkflowID, WorkflowStepID: "step-target",
		FromStepID: "step-a", FromTransitionID: armTransitionID, QueuedAt: time.Now().UTC(),
	}
	if err := queueRepo.SetPendingMove(ctx, session.ID, &move); err != nil {
		t.Fatal(err)
	}

	// A later explicit move leaves step-a and returns to it before the deferred
	// move's atomic admission runs.
	moveTo := func(from, to string) {
		cur, err := repo.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateTaskWithWorkflowStepAdmission(ctx, cur, from, to, 0); err != nil {
			t.Fatal(err)
		}
	}
	moveTo("step-a", "step-b")
	moveTo("step-b", "step-a")

	cur, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, applied, err := repo.UpdateTaskWithWorkflowStepAdmissionForDeferredMove(
		ctx, cur, "step-a", "step-target", 0,
		messagequeue.PendingMoveRecord{SessionID: session.ID, Move: move},
	)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("stale deferred move must not apply after a later move left and returned to its source step")
	}
	stored, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorkflowStepID != "step-a" {
		t.Fatalf("task step = %q, want step-a after dropping the stale deferred move", stored.WorkflowStepID)
	}
	if pending, err := queueRepo.GetPendingMove(ctx, session.ID); err != nil {
		t.Fatal(err)
	} else if pending == nil || pending.MoveID != move.MoveID {
		t.Fatalf("stale deferred move row must be preserved for the caller to drop, got %+v", pending)
	}
}

// A deferred move with no intervening later move still applies: the ledger id
// is unchanged, so the atomic admission proceeds to the target step.
func TestUpdateTaskWithWorkflowStepAdmissionForDeferredMoveAppliesWithoutLaterMove(t *testing.T) {
	repo := newRepoForEntityTests(t)
	ctx := context.Background()
	seedWorkspace(t, repo, "workspace-deferred-unchanged")
	if err := repo.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-deferred-unchanged", WorkspaceID: "workspace-deferred-unchanged", Name: "Workflow"}); err != nil {
		t.Fatal(err)
	}
	seedCASWorkflowStep(t, repo, "workflow-deferred-unchanged", "step-source-unchanged", 0)
	seedCASWorkflowStep(t, repo, "workflow-deferred-unchanged", "step-target-unchanged", 1)

	task := &models.Task{
		ID: "task-deferred-unchanged", WorkspaceID: "workspace-deferred-unchanged", WorkflowID: "workflow-deferred-unchanged",
		WorkflowStepID: "step-source-unchanged", Title: "Deferred candidate", WIPAdmitted: true,
	}
	if err := repo.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	session := &models.TaskSession{
		ID: "session-deferred-unchanged", TaskID: task.ID, QueueIncarnationID: "incarnation-unchanged",
		State: models.TaskSessionStateWaitingForInput, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.CreateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	armTransitionID, err := repo.GetLatestTaskStepTransitionID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	queueRepo, err := messagequeue.NewSQLiteRepository(repo.db, repo.ro)
	if err != nil {
		t.Fatal(err)
	}
	move := messagequeue.PendingMove{
		MoveID: "move-deferred-unchanged", SessionIncarnationID: session.QueueIncarnationID,
		TaskID: task.ID, WorkflowID: task.WorkflowID, WorkflowStepID: "step-target-unchanged",
		FromStepID: "step-source-unchanged", FromTransitionID: armTransitionID, QueuedAt: time.Now().UTC(),
	}
	if err := queueRepo.SetPendingMove(ctx, session.ID, &move); err != nil {
		t.Fatal(err)
	}
	cur, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, applied, err := repo.UpdateTaskWithWorkflowStepAdmissionForDeferredMove(
		ctx, cur, "step-source-unchanged", "step-target-unchanged", 0,
		messagequeue.PendingMoveRecord{SessionID: session.ID, Move: move},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("deferred move with no later move must still apply")
	}
	stored, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorkflowStepID != "step-target-unchanged" {
		t.Fatalf("task step = %q, want step-target-unchanged", stored.WorkflowStepID)
	}
}
