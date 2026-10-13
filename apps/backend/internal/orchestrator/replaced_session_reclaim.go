package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

type replacedSessionReclaim struct {
	taskID      string
	successorID string
	executionID string
}

// Queue ownership and primary promotion register exact old executions. A turn
// already in flight keeps its execution until a later maintenance pass.
func (s *Service) reclaimReplacedSession(ctx context.Context, taskID, oldID, successorID string) {
	if claim, ok := s.registerReplacedSessionReclaim(ctx, taskID, oldID, successorID); ok {
		s.stopReplacedSessionIfSettled(ctx, oldID, claim)
	}
}

func (s *Service) registerReplacedSessionReclaim(ctx context.Context, taskID, oldID, successorID string) (replacedSessionReclaim, bool) {
	if s.agentManager == nil || s.executor == nil || oldID == "" || oldID == successorID {
		return replacedSessionReclaim{}, false
	}
	row, err := s.repo.GetExecutorRunningBySessionID(ctx, oldID)
	if err != nil || row == nil || row.TaskID != taskID || row.AgentExecutionID == "" ||
		row.Status == models.ExecutorRunningStatusStopped {
		return replacedSessionReclaim{}, false
	}
	claim := replacedSessionReclaim{taskID: taskID, successorID: successorID, executionID: row.AgentExecutionID}
	s.replacedSessionReclaims.Store(oldID, claim)
	return claim, true
}

func (s *Service) reclaimReplacedSessionsOnce(ctx context.Context) {
	s.replacedSessionReclaims.Range(func(key, value interface{}) bool {
		s.stopReplacedSessionIfSettled(ctx, key.(string), value.(replacedSessionReclaim))
		return ctx.Err() == nil
	})
}

func (s *Service) stopReplacedSessionIfSettled(ctx context.Context, sessionID string, claim replacedSessionReclaim) {
	var result executor.SessionStopResult
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
		result, err = s.executor.StopSessionDetailed(admittedCtx, session, "session replaced", false)
		return err
	})
	if err != nil {
		s.logger.Warn("replaced session cleanup deferred", zap.String("session_id", sessionID), zap.Error(err))
		return
	}
	if result.ScheduleTeardown() {
		s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
	} else if result.ExecutionID != "" {
		if err := s.executor.StopExecution(ctx, result.ExecutionID, "session replaced", false); err == nil {
			s.replacedSessionReclaims.CompareAndDelete(sessionID, claim)
		}
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
	if !s.replacedSessionReclaimAllowed(ctx, session) {
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

func (s *Service) replacedSessionReclaimAllowed(ctx context.Context, session *models.TaskSession) bool {
	if s.sessionHasKnownIdleWork(ctx, session.ID) {
		return false
	}
	if _, parked := models.LoadWorkflowParking(session.Metadata); parked {
		return false
	}
	isOfficeTask, err := s.lookupOfficeTask(ctx, session.TaskID)
	if err != nil || isOfficeTask {
		return false
	}
	task, err := s.repo.GetTask(ctx, session.TaskID)
	if err != nil || task == nil || task.ArchivedAt != nil {
		return false
	}
	if allowed, _ := s.autoResumeEligibility(ctx, task, session); !allowed {
		return false
	}
	return idleTaskSessionState(session.State) || session.State == models.TaskSessionStateRunning ||
		session.State == models.TaskSessionStateFailed || session.State == models.TaskSessionStateCancelled
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
