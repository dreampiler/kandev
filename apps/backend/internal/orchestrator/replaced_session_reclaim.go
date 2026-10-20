package orchestrator

import (
	"context"
	"fmt"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// replacedSessionReclaim names the exact execution a session held when the
// user promoted another session over it. Only that execution is stopped, and
// only once the old session has settled.
type replacedSessionReclaim struct {
	taskID      string
	successorID string
	executionID string
}

// PromotePrimarySession is the explicit user promotion (session.set_primary).
// It promotes like SetPrimarySession and, when the replaced-session reclaim
// feature is enabled, registers the session it replaced. The stop never runs
// inline: the idle-reaper tick performs it once the old session has settled.
// Automatic promotion, workflow switches, and deletes call SetPrimarySession
// and never register, so they have no effect on the old session's runtime.
func (s *Service) PromotePrimarySession(ctx context.Context, sessionID string) error {
	if !s.config.ReplacedSessionReclaim {
		return s.SetPrimarySession(ctx, sessionID)
	}
	if err := s.authorizeSession(ctx, sessionID); err != nil {
		return err
	}
	taskID, previousPrimaryID, err := s.setSessionPrimaryRecordingPrevious(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to set session as primary: %w", err)
	}
	s.publishPrimarySessionUpdate(ctx, "", sessionID)
	s.registerReplacedSessionReclaim(ctx, taskID, previousPrimaryID, sessionID)
	return nil
}

func (s *Service) registerReplacedSessionReclaim(ctx context.Context, taskID, oldID, successorID string) {
	if !s.config.ReplacedSessionReclaim || s.agentManager == nil || s.executor == nil ||
		oldID == "" || oldID == successorID {
		return
	}
	row, err := s.repo.GetExecutorRunningBySessionID(ctx, oldID)
	if err != nil || row == nil || row.TaskID != taskID || row.AgentExecutionID == "" ||
		row.Status == models.ExecutorRunningStatusStopped {
		return
	}
	s.replacedSessionReclaims.Store(oldID, replacedSessionReclaim{
		taskID: taskID, successorID: successorID, executionID: row.AgentExecutionID,
	})
}

func (s *Service) reclaimReplacedSessionsOnce(ctx context.Context) {
	if !s.config.ReplacedSessionReclaim {
		return
	}
	s.replacedSessionReclaims.Range(func(key, value interface{}) bool {
		s.stopReplacedSessionIfSettled(ctx, key.(string), value.(replacedSessionReclaim))
		return ctx.Err() == nil
	})
}

// stopReplacedSessionIfSettled stops only the registered execution, through
// StopExecution and without a session state write. The session stays in its
// resumable WAITING_FOR_INPUT or IDLE state, which the agent.stopped handler
// preserves, so the conversation resumes on the next prompt.
func (s *Service) stopReplacedSessionIfSettled(ctx context.Context, sessionID string, claim replacedSessionReclaim) {
	stopped := false
	operationCtx, cancel := context.WithTimeout(ctx, idleSuspensionRevalidationTimeout)
	defer cancel()
	err := s.withSessionPromptAdmission(operationCtx, sessionID, func(admittedCtx context.Context) error {
		release, acquired := s.tryAcquireSessionLifecycleLock(sessionID)
		if !acquired {
			return nil
		}
		defer release()
		s.primarySessionReclaimMu.Lock()
		defer s.primarySessionReclaimMu.Unlock()
		session, err := s.replacedSessionForStop(admittedCtx, sessionID, claim)
		if err != nil || session == nil {
			return err
		}
		err = s.executor.StopExecution(admittedCtx, claim.executionID, "session replaced", false)
		// A runtime that is already gone needs no stop.
		if err == nil || agentruntime.IsNotFound(err) {
			stopped = true
			return nil
		}
		return err
	})
	if err != nil {
		s.logger.Warn("replaced session cleanup deferred", zap.String("session_id", sessionID), zap.Error(err))
		return
	}
	if stopped {
		s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
	}
}

func (s *Service) replacedSessionForStop(ctx context.Context, sessionID string, claim replacedSessionReclaim) (*models.TaskSession, error) {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		if isTaskSessionNotFound(err) {
			s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
		}
		return nil, err
	}
	if session == nil || session.IsPrimary || session.TaskID != claim.taskID {
		s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
		return nil, nil
	}
	successor, err := s.repo.GetTaskSession(ctx, claim.successorID)
	if err != nil || successor == nil || successor.TaskID != claim.taskID {
		if isTaskSessionNotFound(err) || err == nil {
			s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
		}
		return nil, err
	}
	switch s.replacedSessionReclaimEligibility(ctx, session) {
	case replacedReclaimDrop:
		s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
		return nil, nil
	case replacedReclaimWait:
		return nil, nil
	}
	current, err := s.replacedExecutionIsCurrent(ctx, sessionID, claim)
	if err != nil || !current {
		return nil, err
	}
	return session, nil
}

func (s *Service) replacedExecutionIsCurrent(ctx context.Context, sessionID string, claim replacedSessionReclaim) (bool, error) {
	row, err := s.repo.GetExecutorRunningBySessionID(ctx, sessionID)
	if err != nil || row == nil {
		return false, err
	}
	if row.AgentExecutionID != claim.executionID || row.Status == models.ExecutorRunningStatusStopped {
		s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
		return false, nil
	}
	currentID, err := s.agentManager.GetExecutionIDForSession(ctx, sessionID)
	return err == nil && currentID == claim.executionID, err
}

type replacedReclaimEligibility int

const (
	// replacedReclaimStop: the session has settled and may be stopped now.
	replacedReclaimStop replacedReclaimEligibility = iota
	// replacedReclaimWait: a transient condition (an active turn, pending work,
	// a session still starting or running, or a failed read) defers the stop to
	// a later tick.
	replacedReclaimWait
	// replacedReclaimDrop: this claim can never stop the session, so the
	// registration is removed instead of being rescanned every tick.
	replacedReclaimDrop
)

func (s *Service) replacedSessionReclaimEligibility(ctx context.Context, session *models.TaskSession) replacedReclaimEligibility {
	if _, parked := models.LoadWorkflowParking(session.Metadata); parked {
		return replacedReclaimDrop
	}
	switch session.State {
	case models.TaskSessionStateWaitingForInput, models.TaskSessionStateIdle:
	case models.TaskSessionStateStarting, models.TaskSessionStateRunning:
		return replacedReclaimWait
	default:
		// A completed, failed, or cancelled session has its own teardown and no
		// resumable state for this stop to preserve.
		return replacedReclaimDrop
	}
	isOfficeTask, err := s.lookupOfficeTask(ctx, session.TaskID)
	if err != nil {
		return replacedReclaimWait
	}
	if isOfficeTask {
		return replacedReclaimDrop
	}
	task, err := s.repo.GetTask(ctx, session.TaskID)
	if err != nil {
		return replacedReclaimWait
	}
	if task == nil || task.ArchivedAt != nil {
		return replacedReclaimDrop
	}
	if allowed, _ := s.autoResumeEligibility(ctx, task, session); !allowed {
		return replacedReclaimDrop
	}
	if s.sessionHasKnownIdleWork(ctx, session.ID) {
		return replacedReclaimWait
	}
	return replacedReclaimStop
}

func (s *Service) previousPrimaryForReplacement(ctx context.Context, sessionID string) (string, string) {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil {
		return "", ""
	}
	primary, err := s.primarySessionID(ctx, session.TaskID)
	if err != nil {
		return session.TaskID, ""
	}
	return session.TaskID, primary
}

func (s *Service) primarySessionID(ctx context.Context, taskID string) (string, error) {
	sessions, err := s.repo.ListTaskSessions(ctx, taskID)
	if err != nil {
		return "", err
	}
	for _, sess := range sessions {
		if sess != nil && sess.IsPrimary {
			return sess.ID, nil
		}
	}
	return "", nil
}

// setSessionPrimaryRecordingPrevious promotes sessionID and returns the task and
// the primary it replaced. The read and the write share one lock with the
// reclaim's stop decision, so a stop never acts on a session that has just been
// promoted again.
func (s *Service) setSessionPrimaryRecordingPrevious(ctx context.Context, sessionID string) (string, string, error) {
	s.primarySessionReclaimMu.Lock()
	defer s.primarySessionReclaimMu.Unlock()
	taskID, previousPrimaryID := s.previousPrimaryForReplacement(ctx, sessionID)
	return taskID, previousPrimaryID, s.repo.SetSessionPrimary(ctx, sessionID)
}

// setSessionPrimary is the SetPrimarySession write. With the reclaim feature on
// it takes the stop decision's lock, so an automatic re-promotion cannot race a
// stop of the session it promotes.
func (s *Service) setSessionPrimary(ctx context.Context, sessionID string) error {
	if !s.config.ReplacedSessionReclaim {
		return s.repo.SetSessionPrimary(ctx, sessionID)
	}
	s.primarySessionReclaimMu.Lock()
	defer s.primarySessionReclaimMu.Unlock()
	return s.repo.SetSessionPrimary(ctx, sessionID)
}
