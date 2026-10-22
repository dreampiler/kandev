package orchestrator

import (
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
)

// continueAutoStartAfterCreatedStateLost handles a CREATED auto-start whose
// session another path moved to a non-terminal state that owns no agent. No
// agent was started for this dispatch, so the step prompt was not delivered.
// It is delivered once through the waiting-session prompt path while the
// session waits for input on the same step; otherwise the dispatch is a no-op
// and a taken hand-off message goes back to the queue.
func (s *Service) continueAutoStartAfterCreatedStateLost(
	state *autoStartStepPromptState,
	fresh *models.TaskSession,
) error {
	onStep, err := s.taskStillOnAutoStartStep(state)
	if err != nil {
		s.requeueAutoStartStepMessage(state)
		return err
	}
	if fresh != nil && fresh.State == models.TaskSessionStateWaitingForInput && onStep {
		s.logger.Info("auto-start: session left CREATED before launch; delivering step prompt to the waiting session",
			zap.String("task_id", state.taskID),
			zap.String("session_id", state.sessionID),
			zap.String("step_name", state.stepName))
		state.session = fresh
		return s.retryAutoStartStepPrompt(state)
	}
	freshState := models.TaskSessionState("")
	if fresh != nil {
		freshState = fresh.State
	}
	s.logger.Info("auto-start: session left CREATED before launch; skipping step prompt",
		zap.String("task_id", state.taskID),
		zap.String("session_id", state.sessionID),
		zap.String("step_name", state.stepName),
		zap.String("session_state", string(freshState)),
		zap.Bool("task_on_step", onStep))
	s.requeueAutoStartStepMessage(state)
	return nil
}

func (s *Service) taskStillOnAutoStartStep(state *autoStartStepPromptState) (bool, error) {
	if state.step == nil {
		return false, nil
	}
	task, err := s.repo.GetTask(state.ctx, state.taskID)
	if err != nil {
		return false, err
	}
	return task != nil && task.WorkflowStepID == state.step.ID, nil
}

func isCreatedStateLostError(err error) bool {
	return errors.Is(err, executor.ErrSessionStateChangedBeforeStart)
}
