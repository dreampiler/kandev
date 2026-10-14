package orchestrator

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

// A vanished turn is not permission to overwrite a replacement execution.
// Only an unsent claim whose durable identity and revision remain unchanged
// may recover its own RUNNING projection.
func (s *Service) restoreUnsentMissingTurnClaim(ctx context.Context, taskID, sessionID string, rollback promptClaimRollback) {
	if rollback.dispatchAccepted || rollback.reservedTurnAccepted || rollback.claimedSessionUpdatedAt.IsZero() || rollback.previousSessionState == models.TaskSessionStateRunning {
		return
	}
	current, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || !ownsUnsentMissingTurnSnapshot(current, taskID, sessionID, rollback) {
		return
	}
	if cached, ok := s.activeTurns.Load(sessionID); ok && cached != rollback.turnID {
		return
	}
	writer, ok := s.repo.(interface {
		UpdateTaskSessionIfCurrentSnapshot(context.Context, *models.TaskSession, models.TaskSessionState, time.Time, map[string]interface{}) (bool, error)
	})
	if !ok {
		return
	}
	current.State = rollback.previousSessionState
	current.ErrorMessage = ""
	current.CompletedAt = nil
	restored, err := writer.UpdateTaskSessionIfCurrentSnapshot(ctx, current, models.TaskSessionStateRunning, rollback.claimedSessionUpdatedAt, current.Metadata)
	if err != nil {
		s.logger.Warn("failed to restore unsent prompt claim after owned turn disappeared", zap.String("task_id", taskID), zap.String("session_id", sessionID), zap.String("turn_id", rollback.turnID), zap.Error(err))
		return
	}
	if !restored {
		return
	}
	s.activeTurns.CompareAndDelete(sessionID, rollback.turnID)
	s.publishTaskSessionStateChanged(ctx, taskID, sessionID, models.TaskSessionStateRunning, rollback.previousSessionState, "", &current.UpdatedAt, current)
	s.republishTaskActivityOnSettle(ctx, taskID, models.TaskSessionStateRunning, rollback.previousSessionState)
	if rollback.taskStateClaimed {
		s.restoreLifecycleTaskState(ctx, taskID, rollback.previousTaskState)
	}
}

// ownsUnsentMissingTurnSnapshot checks the immutable claim against the current session.
func ownsUnsentMissingTurnSnapshot(current *models.TaskSession, taskID, sessionID string, rollback promptClaimRollback) bool {
	identity := rollback.sessionIdentity
	return current != nil && current.TaskID == taskID && identity.TaskID == taskID &&
		identity.SessionID == sessionID && identity.SessionIncarnationID != "" && current.QueueIncarnationID == identity.SessionIncarnationID &&
		current.State == models.TaskSessionStateRunning && current.AgentExecutionID == rollback.claimedExecutionID &&
		current.ErrorMessage == "" && current.CompletedAt == nil && current.UpdatedAt.Equal(rollback.claimedSessionUpdatedAt)
}
