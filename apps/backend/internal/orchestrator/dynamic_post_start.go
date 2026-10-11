package orchestrator

import (
	"context"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
)

// isPostStartUnknownFailure reports whether the classifier left a post-start
// runtime failure unknown, so no provider rule explains it.
func isPostStartUnknownFailure(classified *routingerr.Error) bool {
	return classified != nil && classified.Code == routingerr.CodeAgentRuntime &&
		classified.ClassifierRule == routingerr.PostStartUnknownRule
}

// unclassifiedPostStartEvidence builds evidence for a post-start failure that
// arrived before any result. It carries the failure message so the dynamic
// engine can recognize a known producer shape; only the resulting fingerprint
// is persisted.
func (s *Service) unclassifiedPostStartEvidence(
	ctx context.Context,
	data watcher.AgentEventData,
	session *models.TaskSession,
	phase routingerr.Phase,
) dynamicruntime.UnclassifiedFailureEvidence {
	currentAttempt := data.EvidenceKnown && data.AgentExecutionID != "" && data.PromptGeneration != 0 &&
		s.currentDynamicPromptAttempt(data.SessionID, data.AgentExecutionID, data.PromptGeneration)
	return s.unclassifiedFailureEvidence(
		ctx, data, session, currentAttempt, dynamicruntime.UnclassifiedOriginPostStartNoResult,
		phase, promptUnclassifiedAttemptID(data), "", matchingProviderFailureMessage(data), false,
		data.EvidenceKnown, data.OutputObserved, data.EffectObserved,
	)
}
