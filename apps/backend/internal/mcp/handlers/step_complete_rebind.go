package handlers

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// StepCompleteSessionRebinder promotes a session to its task's primary session.
// It is implemented by *orchestrator.Service. The handler needs only the primary
// binding, so it depends on this narrow surface instead of the orchestrator.
type StepCompleteSessionRebinder interface {
	SetPrimarySession(ctx context.Context, sessionID string) error
}

// taskSessionLister is implemented by the production session repository. It is
// optional so existing handler fakes keep compiling; when it is absent the
// rebind path cannot inspect the designated session and therefore fails closed.
type taskSessionLister interface {
	ListTaskSessions(ctx context.Context, taskID string) ([]*models.TaskSession, error)
}

// ensureStepCompleteSessionOwnership accepts the signal when the calling session
// already owns the current step, or when the session the task currently
// designates for that step is gone or running a different profile and the caller
// can be rebound to it. Any other caller keeps the historical rejection,
// extended with the reason the rebind did not apply.
//
// The check never waits or retries: an unverifiable binding is refused in this
// call, so a broken binding fails closed instead of leaving the step waiting.
func (h *Handlers) ensureStepCompleteSessionOwnership(
	ctx context.Context,
	msg *ws.Message,
	session *models.TaskSession,
	task *models.Task,
	launchStepID string,
) (*ws.Message, error) {
	ownership := h.resolveCurrentStepSessionOwnership(ctx, session, task)
	if ownership.Owned {
		return nil, nil
	}
	rebound, rebindReason, err := h.rebindStepCompleteSession(ctx, session, task, launchStepID)
	if err != nil {
		h.logger.Error("step_complete: failed to rebind session to current step",
			zap.String("task_id", task.ID),
			zap.String("session_id", session.ID),
			zap.Error(err))
		return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeInternalError,
			"failed to rebind session to the current step", nil)
	}
	if rebound {
		return nil, nil
	}
	message := fmt.Sprintf(
		"this session is not the current step's session (current step: %s)", ownership.StepName)
	if ownership.Reason != "" {
		message += ". " + ownership.Reason
	}
	if rebindReason != "" {
		message += ". " + rebindReason
	}
	return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeValidation, message, nil)
}

// rebindStepCompleteSession decides whether the calling session may take over a
// step whose designated session is broken, and promotes it when it may.
//
// It rebinds only when the caller is a live session with an explicit turn stamp
// for the current step and the current step's effective profile, and the
// currently designated (primary) session is missing, inactive, parked, or
// running a different profile. Those conditions keep a same-profile side
// conversation from advancing the workflow and keep a leftover primary from a
// failed profile switch from being overwritten by itself.
func (h *Handlers) rebindStepCompleteSession(
	ctx context.Context,
	session *models.TaskSession,
	task *models.Task,
	launchStepID string,
) (bool, string, error) {
	if h.stepCompleteRebinder == nil {
		return false, "Automatic rebinding is not available on this backend.", nil
	}
	// A primary caller reaches this path only when its profile no longer
	// matches the step, which is exactly the leftover-primary case the profile
	// gate exists to reject. Rebinding it would defeat that guard.
	if session.IsPrimary {
		return false, "", nil
	}
	if !models.IsTaskLookupActiveSessionState(session.State) {
		return false, "This session is not active.", nil
	}
	if _, parked := models.LoadWorkflowParking(session.Metadata); parked {
		return false, "This session was parked for a different workflow step.", nil
	}
	// Only an explicit launch-step stamp proves the caller ran in the current
	// step; the legacy fallback stamp is not strong enough to rebind on.
	if launchStepID == "" || launchStepID != task.WorkflowStepID {
		return false, "", nil
	}
	step, err := h.loadCurrentWorkflowStep(ctx, task.WorkflowStepID)
	if err != nil {
		return false, "", err
	}
	profile, err := h.currentStepAgentProfile(ctx, task, step)
	if err != nil {
		return false, "", err
	}
	if profile != "" && profile != session.AgentProfileID {
		return false, fmt.Sprintf(
			"This session runs agent profile %s, but the current step uses %s.",
			session.AgentProfileID, profile), nil
	}
	primary, err := h.primaryDesignatedSession(ctx, task.ID)
	if err != nil {
		return false, "", err
	}
	if primary != nil && !designatedStepSessionBroken(primary, profile) {
		return false, "Another live session is still the current step's session.", nil
	}
	if err := h.stepCompleteRebinder.SetPrimarySession(ctx, session.ID); err != nil {
		return false, "", err
	}
	h.logger.Info("step_complete: rebound signal to a live current-step session",
		zap.String("task_id", task.ID),
		zap.String("session_id", session.ID),
		zap.String("step_id", task.WorkflowStepID))
	return true, "", nil
}

// primaryDesignatedSession returns the task's current primary session. The
// caller is known not to be primary on this path, so a non-nil result is always
// a different session.
func (h *Handlers) primaryDesignatedSession(ctx context.Context, taskID string) (*models.TaskSession, error) {
	lister, ok := h.sessionRepo.(taskSessionLister)
	if !ok {
		return nil, errors.New("session repository does not support listing sessions")
	}
	sessions, err := lister.ListTaskSessions(ctx, taskID)
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		if sess != nil && sess.IsPrimary {
			return sess, nil
		}
	}
	return nil, nil
}

// designatedStepSessionBroken reports whether the session the task still labels
// primary can no longer own the current step: it is inactive (terminal or idle),
// parked for another step, or running a profile the step no longer uses. A step
// with no effective profile imposes no profile constraint.
func designatedStepSessionBroken(primary *models.TaskSession, stepProfile string) bool {
	if !models.IsTaskLookupActiveSessionState(primary.State) {
		return true
	}
	if _, parked := models.LoadWorkflowParking(primary.Metadata); parked {
		return true
	}
	if stepProfile != "" && primary.AgentProfileID != stepProfile {
		return true
	}
	return false
}
