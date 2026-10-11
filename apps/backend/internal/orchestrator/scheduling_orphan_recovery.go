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
// SCHEDULING-orphan pass needs. It is reached through a type assertion on
// s.repo rather than growing sessionExecutorStore for one startup pass.
type schedulingTaskLister interface {
	ListSchedulingTasks(ctx context.Context) ([]*models.Task, error)
}

// reconcileSchedulingOrphansOnStartup restarts automatic step starts that a
// backend restart interrupted before they produced a running session
// (REQ-TASKS-RESTART-ORPHAN-SESSIONS-003). Such a task is in SCHEDULING with no
// deferred_launch record, no queue destination, and no lifecycle token, so no
// other startup pass lists it: the lifecycle sweep lists durable tokens, the
// WIP queue reconciliation lists queued destinations, the ceiling sweep lists
// deferred records, and the dependency pass lists pending dependency launches.
// Recovery reuses the existing automatic start path, so the session ceiling,
// workflow WIP, step prompt, and session serialization rules all still apply.
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

// schedulingOrphanRecoverable reports whether a SCHEDULING task is an accepted
// automatic start that a restart left without any owner. Every legitimate wait
// is excluded by an existing field or contract, so recovery never forces a
// launch or bypasses the ceiling or WIP limits.
func (s *Service) schedulingOrphanRecoverable(ctx context.Context, task *models.Task) bool {
	if task == nil || task.WorkflowStepID == "" || s.workflowStepGetter == nil {
		return false
	}
	// Waiting for workflow WIP admission: the queue path owns it.
	if task.QueuedForStepID != "" {
		return false
	}
	// A deferred_launch record carries a dependency, WIP overflow, or session
	// ceiling intent. Each has its own retry owner.
	if schedulingOrphanHasDeferredIntent(task) {
		return false
	}
	// A durable lifecycle token already names a recovery owner.
	if schedulingOrphanHasLifecycleToken(task) {
		return false
	}
	// A recorded launch error needs a user decision (model, credential,
	// branch); the task launch recovery action owns it.
	if _, found := models.LoadTaskLaunchError(task.Metadata); found {
		return false
	}
	// Office tasks queue runs, not sessions; their scheduler recovers them.
	if s.isOfficeTask(ctx, task.ID) {
		return false
	}
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil || step == nil {
		return false
	}
	// A step without on_enter auto-start is a manual or hold destination.
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

// schedulingOrphanHasDeferredIntent reports whether the task still carries a
// deferred-launch intent. A non-object or empty record is no intent, matching
// every existing reader of the shared record.
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

// recoverSchedulingOrphan restarts the current step for one orphaned
// SCHEDULING task. A task without an active session goes through the ordinary
// automatic start. A resumable session without prompt history receives the
// step entry in place, so no duplicate session is created. A session that
// already accepted a prompt only has its stale task state repaired. Any other
// active session state means a launch is already live and is left alone.
func (s *Service) recoverSchedulingOrphan(ctx context.Context, task *models.Task) {
	session, err := s.repo.GetActiveTaskSessionByTaskID(ctx, task.ID)
	if err != nil && !errors.Is(err, models.ErrTaskSessionNotFound) {
		s.logger.Warn(schedulingOrphanEvent+": failed to read the active session",
			zap.String("task_id", task.ID), zap.Error(err))
		return
	}
	if session == nil {
		s.logger.Info(schedulingOrphanEvent+": restarting SCHEDULING task with no session",
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
		// state is left. Repair it without sending another prompt.
		s.repairSchedulingOrphanTaskState(ctx, task)
		return
	}
	s.reenterSchedulingOrphanStep(ctx, task, session)
}

// schedulingOrphanResumableSession reports whether an existing session is a
// stopped session that can receive the current step's entry turn. CREATED,
// STARTING, and RUNNING sessions are not resumed: a launch is already live.
func schedulingOrphanResumableSession(session *models.TaskSession) bool {
	if session == nil {
		return false
	}
	return session.State == models.TaskSessionStateWaitingForInput ||
		session.State == models.TaskSessionStateIdle
}

// reenterSchedulingOrphanStep runs the current step's on_enter on the existing
// session, like an ordinary same-step re-entry. on_exit does not run: the task
// never left the step. The entry-dispatch guard inside processOnEnter still
// rejects a stale route, so a task whose committed route no longer names this
// step is left to its owning path.
//
// The entry runs detached, like the launch of a task without a session: it
// resumes the agent and waits for readiness, and doing that inline would hold
// the rest of the startup sweep, including the bounded lifecycle-token pass,
// for up to one launch timeout per task.
func (s *Service) reenterSchedulingOrphanStep(ctx context.Context, task *models.Task, session *models.TaskSession) {
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil || step == nil {
		return
	}
	s.logger.Info(schedulingOrphanEvent+": re-entering the current step on a resumable session",
		zap.String("task_id", task.ID),
		zap.String("session_id", session.ID),
		zap.String("step_id", step.ID))
	entryCtx := context.WithoutCancel(ctx)
	go s.processOnEnter(entryCtx, task.ID, session, step, task.Description, 0, nil)
}

// repairSchedulingOrphanTaskState clears a stale SCHEDULING state for a task
// whose session already accepted a prompt. The turn-start projection that
// would have moved the task to IN_PROGRESS was lost to the same restart, so the
// state is repaired by compare-and-set without running the step again.
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
