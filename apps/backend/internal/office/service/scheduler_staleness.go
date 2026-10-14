package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/office/models"
	"github.com/kandev/kandev/internal/office/shared"
	runsmodels "github.com/kandev/kandev/internal/runs/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// staleRunThreshold is how old a run can be before it is considered stale.
const staleRunThreshold = 2 * time.Hour

// evaluateRunStaleness decides whether a run should be cancelled rather
// than executed. Returns (true, reason) when it should be cancelled.
func (si *SchedulerIntegration) evaluateRunStaleness(
	ctx context.Context,
	run *models.Run,
) (bool, string, error) {
	if run.RetryCount > 0 && !run.RequestedAt.IsZero() &&
		time.Since(run.RequestedAt) > staleRunThreshold {
		return true, "execution_too_old", nil
	}

	// Engine-emitted runs carry the workflow step that produced them. A task
	// can move to another step while the run waits in the queue, so compare
	// the queued step with the task's current step before launch. Legacy runs
	// do not include workflow_step_id and retain the existing behaviour.
	payload := ParseRunPayload(run.Payload)
	taskID := payload["task_id"]
	consumed, err := si.childrenCompletedWakeConsumed(ctx, run, taskID)
	if err != nil {
		return false, "", err
	}
	if consumed {
		return true, staleReasonParentTaskTerminal, nil
	}
	expectedStepID := payload["workflow_step_id"]
	if expectedStepID == "" || taskID == "" {
		return false, "", nil
	}
	if expectedTransitionID, hasEntryIdentity := payload["workflow_step_transition_id"]; hasEntryIdentity {
		entryID, err := strconv.ParseInt(expectedTransitionID, 10, 64)
		if err != nil || entryID < 0 {
			return true, "invalid_workflow_step_entry", nil
		}
		currentStepID, currentEntryID, err := si.svc.repo.GetTaskWorkflowStepEntry(ctx, taskID)
		if err != nil {
			return false, "", fmt.Errorf("resolve current workflow step entry for task %s: %w", taskID, err)
		}
		if currentStepID != expectedStepID || currentEntryID != entryID {
			return true, "workflow_step_entry_changed", nil
		}
		return false, "", nil
	}
	currentStepID, err := si.svc.repo.GetTaskWorkflowStepID(ctx, taskID)
	if err != nil {
		return false, "", fmt.Errorf("resolve current workflow step for task %s: %w", taskID, err)
	}
	if currentStepID != expectedStepID {
		return true, "workflow_step_changed", nil
	}
	return false, "", nil
}

// staleReasonParentTaskTerminal cancels a children-completed wake whose parent
// already reached a terminal state: the coordination it asks for is over, so
// launching it would only start an empty turn.
const staleReasonParentTaskTerminal = "parent_task_terminal"

// childrenCompletedWakeConsumed reports whether a children-completed run has
// nothing left to deliver: its parent task is COMPLETED or CANCELLED — the
// terminal predicate the backstop producer already applies to parents
// (ListStuckParents) — and no event-triggered wakeup request was coalesced
// into it. Periodic routine wakes merged into such a run are released exactly
// as an idle skip would release them; the next cron tick queues a fresh run.
// A read failure is returned so the caller keeps its existing retry path.
func (si *SchedulerIntegration) childrenCompletedWakeConsumed(
	ctx context.Context, run *runsmodels.Run, parentTaskID string,
) (bool, error) {
	if parentTaskID == "" ||
		(run.Reason != RunReasonTaskChildrenCompleted && run.Reason != legacyRunReasonChildrenCompleted) {
		return false, nil
	}
	state, err := si.svc.repo.GetTaskState(ctx, parentTaskID)
	if err != nil {
		return false, fmt.Errorf("resolve parent task state for %s: %w", parentTaskID, err)
	}
	if state != string(v1.TaskStateCompleted) && state != string(v1.TaskStateCancelled) {
		return false, nil
	}
	carriesEvent, err := si.svc.repo.HasCoalescedWakeupOutsideReasons(
		ctx, run.ID, shared.RunReasonRoutineDispatchCron, shared.RunReasonHeartbeat)
	if err != nil {
		return false, fmt.Errorf("inspect wakeups coalesced into run %s: %w", run.ID, err)
	}
	return !carriesEvent, nil
}

// cancelStaleRun marks the run cancelled, logs the event, and releases
// any checkout. Terminal-shape recording, the OfficeRunProcessed
// broadcast, and the activity log entry only happen when the cancel
// actually applied: a run another writer already finished or failed
// must not be reported as cancelled.
func (si *SchedulerIntegration) cancelStaleRun(
	ctx context.Context,
	run *models.Run,
	agent *models.AgentInstance,
	reason string,
) {
	si.logger.Info("cancelling stale run",
		zap.String("run_id", run.ID),
		zap.String("agent", agent.Name),
		zap.String("reason", reason),
		zap.Time("requested_at", run.RequestedAt))

	si.releaseCheckoutIfNeeded(ctx, run)
	si.svc.clearAgentWorking(ctx, agent.ID, run.ID)

	cancelled, err := si.svc.repo.CancelRun(ctx, run.ID, reason)
	if err != nil {
		si.logger.Error("failed to cancel stale run",
			zap.String("run_id", run.ID), zap.Error(err))
		return
	}
	if !cancelled {
		return
	}

	si.svc.recordTerminalShape(ctx, run, RunStatusCancelled, nil)
	si.svc.publishRunProcessed(ctx, run.ID, RunStatusCancelled, run)

	si.svc.LogActivityWithRun(ctx, agent.WorkspaceID,
		"scheduler", "office-scheduler",
		"run_cancelled_stale", "run", run.ID,
		mustJSON(map[string]string{
			"agent":    agent.Name,
			"agent_id": agent.ID,
			"reason":   reason,
		}), run.ID, "")
}
