package acp

import (
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"go.uber.org/zap"
)

// observeUsageLimitNotice correlates a usage-limit notice the agent stated as
// an ordinary assistant message with the in-flight prompt turn.
//
// Some providers (Antigravity's agy ACP server among them) announce an
// exhausted quota as assistant text and then never settle session/prompt. The
// notice is therefore the only failure signal, and without it the turn holds a
// session-ceiling slot until the process dies. Handing the diagnostic to the
// turn reuses the existing prompt-settlement path: waitForPromptRPCAfterUserCancel
// already selects on this channel, ends the turn with a providerPromptError,
// and releases the prompt gate through the configured cancel timeout.
//
// The notice arrives in more than one block often enough that this check reads
// the turn's accumulated assistant text, not the single chunk: a notice split
// mid-sentence would otherwise never match. Only an assistant chunk may
// contribute, since a user quoting a notice must never end the agent's turn.
func (a *Adapter) observeUsageLimitNotice(sessionID, role, text string) {
	if a.agentID == "" || role == acpUserRole || text == "" {
		return
	}
	turn := a.currentPromptTurn()
	if turn == nil || turn.promptGeneration == 0 {
		return
	}
	buffer := turn.appendQuotaNoticeText(text)
	if buffer == "" {
		return
	}
	// The buffer stops growing at quotaNoticeBufferBytes, so for the rest of
	// the turn every chunk would otherwise re-run the whole rule engine over
	// byte-identical input. Classify once per distinct buffer and reuse it.
	classified, ok := turn.quotaNoticeClassification(buffer, a.agentID)
	if !ok {
		return
	}
	if !isUsageLimitNotice(classified) {
		return
	}
	message := streams.SanitizeProviderMessage(buffer)
	if message == "" {
		return
	}
	diagnostic := providerNoticeDiagnostic{
		SessionID: sessionID,
		ProviderError: streams.ProviderError{
			Source:                     streams.ProviderErrorSourceAgentMessage,
			ProviderID:                 a.agentID,
			Message:                    message,
			DiagnosticIdentityComplete: streams.IsCompleteProviderDiagnostic(buffer),
			OccurredAt:                 time.Now().UTC(),
			ResetAt:                    classified.ResetHint,
		},
	}
	select {
	case turn.providerErrorCh <- diagnostic:
		a.logger.Info("usage-limit notice stated as an agent message; ending the prompt turn",
			zap.String("session_id", sessionID),
			zap.String("agent_id", a.agentID),
			zap.String("classifier_rule", classified.ClassifierRule))
	default:
		a.logger.Debug("dropping usage-limit notice because the prompt diagnostic channel is full",
			zap.String("session_id", sessionID))
	}
}

// isUsageLimitNotice reports whether a classification is an exhausted-capacity
// failure strong enough to end a turn. A lower-confidence match is left as an
// ordinary diagnostic candidate: an agent describing a limit it read about is
// not itself a limit failure.
func isUsageLimitNotice(classified *routingerr.Error) bool {
	if classified == nil || classified.Confidence != routingerr.ConfHigh || !classified.FallbackAllowed {
		return false
	}
	return classified.Code == routingerr.CodeQuotaLimited || classified.Code == routingerr.CodeRateLimited
}
