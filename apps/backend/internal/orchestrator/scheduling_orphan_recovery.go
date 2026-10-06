package orchestrator

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	workflowmove "github.com/kandev/kandev/internal/workflow/move"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// schedulingOrphanEvent names the startup cause in logs and launch dispatch.
const schedulingOrphanEvent = "startup.scheduling_orphan"

// schedulingTaskLister is the narrow repository capability the startup
// SCHEDULING-orphan pass needs. Reached through a type assertion on s.repo,
// following the ceilingDeferredTaskLister idiom, rather than growing
// sessionExecutorStore for a capability only this pass uses.
type schedulingTaskLister interface {
	ListSchedulingTasks(ctx context.Context) ([]*models.Task, error)
}

// reconcileSchedulingOrphansOnStartup re-drives tasks left in SCHEDULING by a
// backend restart or rollback while a launch was in flight. Such a task has no
// deferred_launch record, no queue destination, and no lifecycle token, so no
// other startup pass lists it: the lifecycle sweep only lists durable tokens,
// the WIP queue reconciliation only lists QueuedForStepID rows, and the ceiling
// sweep only lists deferred records. Recovery reuses the existing launch
// chokepoint so the session ceiling, workflow WIP, model/step prompt, and
// session serialization contracts all still apply.
//
// It runs before the durable-token sweep so a task that a token owns is still
// token-bearing here and is skipped, rather than being seen as an orphan after
// its token was cleared but before its detached launch created a session.
func (s *Service) reconcileSchedulingOrphansOnStartup(ctx context.Context) {
	lister, ok := s.repo.(schedulingTaskLister)
	if !ok {
		return
	}
	tasks, err := lister.ListSchedulingTasks(ctx)
	if err != nil {
		s.logger.Warn("startup: failed to list SCHEDULING tasks for orphan recovery", zap.Error(err))
		return
	}
	for _, task := range tasks {
		if s.schedulingOrphanRecoverable(ctx, task) {
			s.recoverSchedulingOrphan(ctx, task)
		}
	}
}

// schedulingOrphanRecoverable reports whether a SCHEDULING task is an actual
// execution target a restart left without any owner. Every legitimate wait is
// excluded by an existing field or contract, so recovery never forces a launch
// or bypasses the ceiling/WIP limits.
func (s *Service) schedulingOrphanRecoverable(ctx context.Context, task *models.Task) bool {
	if task == nil || task.WorkflowStepID == "" || s.workflowStepGetter == nil {
		return false
	}
	// Waiting for workflow WIP admission: the queue path owns it.
	if task.QueuedForStepID != "" {
		return false
	}
	// A deferred_launch record carries one of three durable intents (dependency
	// chain, WIP overflow, or the session ceiling). Each has its own retry
	// owner, so forcing a launch here would bypass it.
	if schedulingOrphanHasDeferredIntent(task) {
		return false
	}
	// A durable lifecycle token already names a recovery owner.
	if schedulingOrphanHasLifecycleToken(task) {
		return false
	}
	// A recorded launch error is a user decision (model, credential, branch);
	// the task.launch.recover action owns it.
	if _, found := models.LoadTaskLaunchError(task.Metadata); found {
		return false
	}
	// Office tasks queue runs, not sessions; their scheduler recovery owns them.
	if s.isOfficeTask(ctx, task.ID) {
		return false
	}
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil || step == nil {
		return false
	}
	// A step without on_enter auto-start is a manual/hold destination, not a
	// launch target.
	if !workflowmove.ShouldAutoStartAgent(step, nil) {
		return false
	}
	// A dependency-blocked task must not start; the dependency sweep launches
	// it once its predecessor resolves.
	if blocked, _ := s.dependencyBlocksAutoStart(ctx, task.ID, schedulingOrphanEvent); blocked {
		return false
	}
	return true
}

// schedulingOrphanHasDeferredIntent reports whether the task still carries any
// durable deferred-launch intent. A non-object or empty record is no intent,
// matching every existing reader of the shared record.
func schedulingOrphanHasDeferredIntent(task *models.Task) bool {
	if task == nil || task.Metadata == nil {
		return false
	}
	record, ok := task.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	return ok && len(record) > 0
}

// schedulingOrphanHasLifecycleToken reports whether another startup pass
// already owns this task through a durable token.
func schedulingOrphanHasLifecycleToken(task *models.Task) bool {
	if task == nil || task.Metadata == nil {
		return false
	}
	if queuedMoveExitPending(task) || manualMoveLifecyclePending(task) ||
		manualMoveLifecycleCompleted(task) || hasQueuePromotionPending(task) {
		return true
	}
	return autoStartOnCreateActionable(task)
}

// recoverSchedulingOrphan re-drives the current step for one orphaned
// SCHEDULING task. A task whose current step already holds a resumable session
// with no prompt history is re-entered in place so no duplicate session is
// created; one without a session goes through the ordinary no-session
// auto-start path. Any other active session state means a launch is already
// live and is left alone.
func (s *Service) recoverSchedulingOrphan(ctx context.Context, task *models.Task) {
	session, err := s.repo.GetActiveTaskSessionByTaskID(ctx, task.ID)
	if err != nil && !errors.Is(err, models.ErrTaskSessionNotFound) {
		s.logger.Warn(schedulingOrphanEvent+": failed to read the active session",
			zap.String("task_id", task.ID), zap.Error(err))
		return
	}
	if session == nil {
		s.logger.Info(schedulingOrphanEvent+": re-driving SCHEDULING task with no session",
			zap.String("task_id", task.ID),
			zap.String("step_id", task.WorkflowStepID))
		s.autoStartTaskForStep(ctx, task.ID, task.WorkflowStepID, schedulingOrphanEvent, 0, false)
		return
	}
	if !schedulingOrphanResumableSession(session) {
		return
	}
	hasPrompt, err := s.repo.HasUserPromptHistory(ctx, session.ID)
	if err != nil {
		s.logger.Warn(schedulingOrphanEvent+": failed to read session prompt history",
			zap.String("task_id", task.ID), zap.String("session_id", session.ID), zap.Error(err))
		return
	}
	if hasPrompt {
		// The step entry already delivered a turn; only the stale SCHEDULING
		// state is left. Repair it without sending a second prompt.
		s.repairSchedulingOrphanTaskState(ctx, task)
		return
	}
	s.reenterSchedulingOrphanStep(ctx, task, session)
}

// schedulingOrphanResumableSession reports whether an existing session is a
// stopped-on-purpose session that can receive the current step's entry turn.
// STARTING/RUNNING/CREATED sessions are not resumed: a launch is already live
// for them.
func schedulingOrphanResumableSession(session *models.TaskSession) bool {
	if session == nil {
		return false
	}
	return session.State == models.TaskSessionStateWaitingForInput ||
		session.State == models.TaskSessionStateIdle
}

// reenterSchedulingOrphanStep delivers the current step's on_enter to the
// existing session, matching an ordinary same-step re-entry. on_exit is not
// run: the task never left the step, so running it would repeat source-side
// effects a re-entry is not meant to repeat. The entry-dispatch guard inside
// processOnEnter still rejects a stale route, so a task whose committed route
// no longer names this step is left for its owning path instead of forced.
func (s *Service) reenterSchedulingOrphanStep(ctx context.Context, task *models.Task, session *models.TaskSession) {
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil || step == nil {
		return
	}
	s.logger.Info(schedulingOrphanEvent+": re-entering the current step for a resumable session",
		zap.String("task_id", task.ID),
		zap.String("session_id", session.ID),
		zap.String("step_id", step.ID))
	s.processOnEnter(ctx, task.ID, session, step, task.Description, 0, nil)
}

// repairSchedulingOrphanTaskState clears a stale SCHEDULING state for a task
// whose session already accepted a prompt. The turn-start projection that would
// normally have moved the task to IN_PROGRESS was lost to the same restart, so
// the state is repaired by compare-and-set without re-running the step.
func (s *Service) repairSchedulingOrphanTaskState(ctx context.Context, task *models.Task) {
	updated, err := s.taskRepo.UpdateTaskStateIfCurrentIn(
		ctx, task.ID, v1.TaskStateInProgress, []v1.TaskState{v1.TaskStateScheduling},
	)
	if err != nil {
		s.logger.Warn(schedulingOrphanEvent+": failed to repair stale SCHEDULING task state",
			zap.String("task_id", task.ID), zap.Error(err))
		return
	}
	if updated {
		s.logger.Info(schedulingOrphanEvent+": repaired stale SCHEDULING task state for a session with prompt history",
			zap.String("task_id", task.ID))
	}
}
