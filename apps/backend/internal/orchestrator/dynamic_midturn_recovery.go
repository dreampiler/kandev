package orchestrator

import (
	"context"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
)

func (s *Service) interruptedDynamicFailure(ctx context.Context, data watcher.AgentEventData, session *models.TaskSession, failure *routingerr.Error) bool {
	if !dynamicruntime.InterruptedFailureAllowed(failure) || !data.EvidenceKnown ||
		(!data.OutputObserved && !data.EffectObserved) || !s.currentInterruptedDynamicAttempt(data, session) ||
		isTerminalSessionState(session.State) || s.isCancelInFlight(session.ID) {
		return false
	}
	_, taskOwned := s.unclassifiedFailureTask(ctx, data, session)
	return taskOwned
}

func (s *Service) currentInterruptedDynamicAttempt(data watcher.AgentEventData, session *models.TaskSession) bool {
	if session == nil || data.AgentExecutionID == "" || session.AgentExecutionID != data.AgentExecutionID ||
		data.PromptGeneration == 0 {
		return false
	}
	evidence, ok := s.promptAttemptForSession(session.ID)
	if !ok {
		return false
	}
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	return evidence.dynamic && evidence.evidenceKnown &&
		evidence.executionID == data.AgentExecutionID && evidence.promptGeneration == data.PromptGeneration &&
		evidence.routeGeneration == session.RouteGeneration
}

func (s *Service) preferInterruptedDynamicFailure(ctx context.Context, data watcher.AgentEventData, failure *routingerr.Error) bool {
	session, ok := s.dynamicFailureSession(ctx, data)
	return ok && s.interruptedDynamicFailure(ctx, data, session, failure)
}

// A terminal event can arrive again before the detached worker binds its new
// execution. The predecessor's prompt record remains until that binding.
func (s *Service) supersededInterruptedDynamicAttempt(data watcher.AgentEventData, session *models.TaskSession) bool {
	evidence, ok := s.promptAttemptForSession(data.SessionID)
	if !ok || session == nil || data.PromptGeneration == 0 || data.AgentExecutionID == "" {
		return false
	}
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	return evidence.dynamic && evidence.routeGeneration > 0 &&
		evidence.routeGeneration < session.RouteGeneration && evidence.executionID == data.AgentExecutionID &&
		evidence.promptGeneration == data.PromptGeneration
}
