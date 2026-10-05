package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// stepCompleteOwnership is the answer to one question: is the calling session
// the session the orchestrator currently drives for the task's current
// workflow step? StepName is carried even when ownership could not be
// verified so the rejection can name the step the caller believes it is in.
// Reason is set only on rejection and states why.
type stepCompleteOwnership struct {
	Owned    bool
	StepName string
	Reason   string
}

// resolveCurrentStepSessionOwnership decides whether session may signal
// completion for task's current step.
//
// Two existing designations decide it, and both are maintained by the
// orchestrator on every step entry, so the handler never has to re-derive the
// session/step relationship:
//
//   - session.IsPrimary is the session the task is currently driven by. The
//     keep-the-session path leaves it primary; the switch path promotes the
//     destination session (reuse promotes a nonterminal reusable session,
//     otherwise the newly created session becomes primary). A predecessor
//     that was parked instead of completed therefore stops being primary, and
//     a same-profile side conversation never is.
//   - The current step's effective agent profile, resolved exactly as the
//     orchestrator resolves it, must be the profile this session runs. It is a
//     necessary condition, never a sufficient one: a leftover primary session
//     from a failed profile switch still fails it.
//
// Ownership that cannot be verified is refused: an unreadable current step, or
// a handler without a workflow controller, means there is no evidence that the
// caller owns the step, and a completion signal moves the task.
func (h *Handlers) resolveCurrentStepSessionOwnership(
	ctx context.Context,
	session *models.TaskSession,
	task *models.Task,
) stepCompleteOwnership {
	step, err := h.loadCurrentWorkflowStep(ctx, task.WorkflowStepID)
	if err != nil {
		return stepCompleteOwnership{
			StepName: task.WorkflowStepID,
			Reason: fmt.Sprintf(
				"The current workflow step could not be read, so step ownership cannot be verified (%v).",
				err,
			),
		}
	}
	if !session.IsPrimary {
		return stepCompleteOwnership{
			StepName: step.Name,
			Reason:   "This session is not the task's current session; another session was selected for this step.",
		}
	}
	profile, err := h.currentStepAgentProfile(ctx, task, step)
	if err != nil {
		return stepCompleteOwnership{
			StepName: step.Name,
			Reason: fmt.Sprintf(
				"The current step's agent profile could not be resolved, so step ownership cannot be verified (%v).",
				err,
			),
		}
	}
	if profile != "" && profile != session.AgentProfileID {
		return stepCompleteOwnership{
			StepName: step.Name,
			Reason: fmt.Sprintf(
				"This session runs agent profile %s, but the current step uses %s.",
				session.AgentProfileID, profile,
			),
		}
	}
	return stepCompleteOwnership{Owned: true, StepName: step.Name}
}

var errNoWorkflowController = errors.New("workflow controller is not wired")

func (h *Handlers) loadCurrentWorkflowStep(ctx context.Context, stepID string) (*wfmodels.WorkflowStep, error) {
	if h.workflowCtrl == nil {
		return nil, errNoWorkflowController
	}
	resp, err := h.workflowCtrl.GetStep(ctx, stepID)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Step == nil {
		return nil, fmt.Errorf("workflow step %s not found", stepID)
	}
	return resp.Step, nil
}

// currentStepAgentProfile mirrors the orchestrator's own step-profile
// resolution (resolveStepAgentProfileForTask): an explicit session target owns
// the conversation and pins no profile, then the task's fixed-step
// substitution, then the step's own profile, then the workflow default. An
// empty result means the step keeps whichever session it already has, so the
// caller cannot be rejected on profile grounds.
func (h *Handlers) currentStepAgentProfile(
	ctx context.Context,
	task *models.Task,
	step *wfmodels.WorkflowStep,
) (string, error) {
	if step.SessionTarget != nil {
		return "", nil
	}
	if replacement, ok := task.WorkflowAgentOverrides.ReplacementFor(task.WorkflowID, step.ID); ok {
		return replacement, nil
	}
	if step.AgentProfileID != "" {
		return step.AgentProfileID, nil
	}
	if step.WorkflowID == "" {
		return "", nil
	}
	return h.workflowDefaultAgentProfileWithError(ctx, step.WorkflowID)
}
