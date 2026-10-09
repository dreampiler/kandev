package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	workflowmove "github.com/kandev/kandev/internal/workflow/move"
)

// DiscardDeferredCeilingLaunch drops a task's queued automatic launch at an
// operator's request. An automatic launch deferred by the session ceiling can
// become permanently unreplayable — the runtime it needs was torn down and
// nothing relaunches it — yet its durable record keeps every later automatic
// launch for the task refused as a conflict. This is the escape hatch that
// releases that hold without waiting for the sweep to drain it.
//
// The record is disposed of exactly like a dropped replay: the automation run
// it serves is failed, a card note is written when a session carries one, and
// the durable record is cleared. discarded reports whether the record was
// actually cleared, so a task left holding the record because its automation
// run could not be failed is not reported as released. A task with no queued
// ceiling launch is a no-op, so a retried release is idempotent.
func (s *Service) DiscardDeferredCeilingLaunch(ctx context.Context, taskID string) (bool, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	if task == nil {
		return false, nil
	}
	deferral, present, err := s.readPendingCeilingDeferral(ctx, taskID)
	if err != nil || !present {
		return false, err
	}
	s.dropCeilingDeferral(ctx, task, sessionIDFromCeilingPayload(deferral), deferral,
		ceilingReasonDroppedManualRelease, "an operator released the deferred launch")
	// dropCeilingDeferral keeps the record when the automation run it serves
	// cannot be failed, so re-read to report the durable outcome rather than
	// the attempt.
	remaining, _, readErr := s.repo.GetTaskDeferredLaunch(ctx, taskID)
	if readErr != nil {
		return false, readErr
	}
	return !ceilingDeferredRecordPresent(remaining), nil
}

// readPendingCeilingDeferral reads a task's shared deferred_launch record and
// reports the ceiling half when it is present and decodable. A record with no
// ceiling flag is a valid negative result for every non-ceiling owner of the
// shared record.
func (s *Service) readPendingCeilingDeferral(ctx context.Context, taskID string) (models.CeilingDeferral, bool, error) {
	record, _, err := s.repo.GetTaskDeferredLaunch(ctx, taskID)
	if err != nil {
		return models.CeilingDeferral{}, false, err
	}
	if !ceilingDeferredRecordPresent(record) {
		return models.CeilingDeferral{}, false, nil
	}
	deferral, readErr := models.ReadCeilingDeferral(record)
	if readErr != nil {
		return models.CeilingDeferral{}, false, readErr
	}
	return deferral, true, nil
}

func ceilingDeferredRecordPresent(record map[string]interface{}) bool {
	if record == nil {
		return false
	}
	flag, ok := record[models.CeilingDeferredKey]
	return ok && flag == true
}

// dropCeilingStartOnNonAutoStartMove clears a task's queued ceiling "start"
// when the task has just moved into a step that cannot run an agent, without
// waiting for the next sweep. A start cannot launch from a hold or a
// predecessor wait, so keeping it queued only lets the stale record rank as
// the queue head when the task returns to an agent step. The ceiling keys are
// removed while any coexisting WIP-overflow or dependency intent stays, and a
// non-start record (a resume or prompt for an existing session) is left alone.
func (s *Service) dropCeilingStartOnNonAutoStartMove(
	ctx context.Context,
	taskID string,
	task *models.Task,
	step *wfmodels.WorkflowStep,
) {
	if taskID == "" || task == nil || step == nil {
		return
	}
	if workflowmove.ShouldAutoStartAgent(step, nil) {
		return
	}
	deferral, present, err := s.readPendingCeilingDeferral(ctx, taskID)
	if err != nil || !present {
		if err != nil {
			s.logger.Zap().Warn("could not read deferred launch before clearing a queued start on a non-auto-start move",
				zap.String("task_id", taskID), zap.Error(err))
		}
		return
	}
	if deferral.Kind != models.CeilingLaunchStart {
		return
	}
	s.deferredRetrySchedule.settle(taskID)
	s.dropCeilingDeferral(ctx, task, sessionIDFromCeilingPayload(deferral), deferral,
		ceilingReasonSuperseded, "workflow destination step cannot auto-start after the move")
}
