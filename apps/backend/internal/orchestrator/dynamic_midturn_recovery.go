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

// currentInterruptedDynamicAttempt reports whether a terminal failure belongs to
// the session's live dynamic prompt attempt. The attempt identity captured when
// the prompt began (execution plus prompt generation) is the fence; the session's
// projected execution and the recorded route generation are advisory, because
// either can be empty or advanced while the same turn is still in flight. A
// route the session has already moved past is rejected so a late or duplicate
// failure cannot advance the successor again.
func (s *Service) currentInterruptedDynamicAttempt(data watcher.AgentEventData, session *models.TaskSession) bool {
	if session == nil || data.AgentExecutionID == "" || data.PromptGeneration == 0 {
		return false
	}
	if session.AgentExecutionID != "" && session.AgentExecutionID != data.AgentExecutionID {
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
		!dynamicAttemptSuperseded(evidence, session)
}

// dynamicAttemptSuperseded reports whether the route has already advanced past
// the attempt that failed. Callers must hold evidence.mu.
func dynamicAttemptSuperseded(evidence *promptAttemptEvidence, session *models.TaskSession) bool {
	if evidence.routeGeneration > 0 && evidence.routeGeneration < session.RouteGeneration {
		return true
	}
	return evidence.executionProfileID != "" && session.ExecutionProfileID != "" &&
		evidence.executionProfileID != session.ExecutionProfileID
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
