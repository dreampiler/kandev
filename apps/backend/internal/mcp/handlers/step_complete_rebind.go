package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// stepCompleteRejectionStore is the optional metadata writer used to record why
// a step_complete call was refused. The production session repository
// implements it; a fake that omits it records nothing and keeps its behavior.
type stepCompleteRejectionStore interface {
	SetSessionMetadataKey(ctx context.Context, sessionID, key string, value interface{}) error
}

// stepCompleteRejectionReasonLimit bounds the recorded refusal reason. The
// reminder shows it to the child agent, so it must stay short and free of paths
// or secrets (the gate's messages carry only session/step/profile ids).
const stepCompleteRejectionReasonLimit = 400

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
	// Record the refusal so the child's missing-signal reminder names the real
	// block instead of implying the agent never called step_complete.
	h.recordStepCompleteRejection(ctx, session.ID, task.WorkflowStepID, message)
	return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeValidation, message, nil)
}

// recordStepCompleteRejection stores the refusal reason on the calling session
// for the current step. Best effort: a write failure only means the reminder
// falls back to its generic text.
func (h *Handlers) recordStepCompleteRejection(ctx context.Context, sessionID, stepID, reason string) {
	writer, ok := h.sessionRepo.(stepCompleteRejectionStore)
	if !ok || sessionID == "" || stepID == "" || strings.TrimSpace(reason) == "" {
		return
	}
	rejection := models.StepCompleteRejection{
		StepID: stepID,
		Reason: boundStepCompleteRejectionReason(reason),
		At:     time.Now().UTC(),
	}
	if err := writer.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyStepCompleteRejection, rejection.ToMap()); err != nil {
		h.logger.Debug("step_complete: failed to record the refusal reason",
			zap.String("session_id", sessionID), zap.Error(err))
	}
}

// clearStepCompleteRejection drops the recorded refusal reason once a signal for
// the current step is accepted, so a resolved block is never shown again.
func (h *Handlers) clearStepCompleteRejection(ctx context.Context, sessionID string) {
	writer, ok := h.sessionRepo.(stepCompleteRejectionStore)
	if !ok || sessionID == "" {
		return
	}
	if err := writer.SetSessionMetadataKey(ctx, sessionID, models.SessionMetaKeyStepCompleteRejection, nil); err != nil {
		h.logger.Debug("step_complete: failed to clear the refusal reason",
			zap.String("session_id", sessionID), zap.Error(err))
	}
}

func boundStepCompleteRejectionReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if len(trimmed) <= stepCompleteRejectionReasonLimit {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:stepCompleteRejectionReasonLimit])
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
