package acp

import (
	"strings"
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
// contribute, since a user quoting a notice must never end the agent's turn,
// and the turn's output must open with the notice: an agent that quotes or
// summarizes another session's limit error is reporting, not failing.
func (a *Adapter) observeUsageLimitNotice(promptGeneration uint64, event *AgentEvent) {
	if !a.usageLimitNoticeEventAccepted(promptGeneration, event) {
		return
	}
	turn := a.currentPromptTurn()
	if turn == nil || turn.promptGeneration != promptGeneration {
		return
	}
	a.mu.RLock()
	current := !a.closed && a.sessionID == event.SessionID
	a.mu.RUnlock()
	if !current {
		return
	}
	sessionID := event.SessionID
	buffer := turn.appendQuotaNoticeText(event.Text)
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
	relative := classified.ClassifierRule == routingerr.RuleRelativeQuotaNotice
	complete := quotaNoticeSentenceComplete(buffer)
	if relative && relativeQuotaNoticeContainsOutput(buffer, complete) {
		turn.closeQuotaNotice()
		return
	}
	message := streams.SanitizeProviderMessage(buffer)
	if message == "" {
		return
	}
	event.ProviderDiagnosticCandidate = true
	diagnostic := providerNoticeDiagnostic{
		SessionID: sessionID,
		ProviderError: streams.ProviderError{
			Source:                     streams.ProviderErrorSourceAgentMessage,
			ProviderID:                 a.agentID,
			Message:                    message,
			DiagnosticIdentityComplete: streams.IsCompleteProviderDiagnostic(buffer),
			OccurredAt:                 time.Now().UTC(),
			ResetAt:                    quotaNoticeResetHint(buffer, classified.ResetHint),
		},
	}
	if relative && !complete {
		turn.waitForQuotaNoticeSuffix(a, diagnostic, classified.ClassifierRule)
		return
	}
	turn.deliverQuotaNotice(a, diagnostic, classified.ClassifierRule)
}

func relativeQuotaNoticeContainsOutput(text string, complete bool) bool {
	if complete {
		return !routingerr.IsCompleteRelativeQuotaNoticeText(text)
	}
	return routingerr.RelativeQuotaNoticeHasTrailingOutput(text) || !routingerr.IsRelativeQuotaNoticePrefix(text)
}

func (turn *promptTurnState) waitForQuotaNoticeSuffix(a *Adapter, diagnostic providerNoticeDiagnostic, rule string) {
	turn.evidenceMu.Lock()
	defer turn.evidenceMu.Unlock()
	if turn.quotaNoticeDelivered || turn.quotaNoticeTimer != nil {
		return
	}
	turn.pendingQuotaNotice = &diagnostic
	turn.quotaNoticeTimer = time.AfterFunc(a.promptCancelTimeout(), func() {
		if a.currentPromptTurn() == turn {
			turn.deliverQuotaNotice(a, diagnostic, rule)
		}
	})
}

func (turn *promptTurnState) closeQuotaNotice() {
	turn.evidenceMu.Lock()
	defer turn.evidenceMu.Unlock()
	turn.quotaNoticeDelivered = true
	turn.pendingQuotaNotice = nil
	turn.completedQuotaNotice = nil
	if turn.quotaNoticeTimer != nil {
		turn.quotaNoticeTimer.Stop()
	}
}

func (turn *promptTurnState) quotaNoticeOnNormalPromptReturn(sessionID string) error {
	turn.evidenceMu.Lock()
	defer turn.evidenceMu.Unlock()
	if turn.pendingQuotaNotice != nil && turn.pendingQuotaNotice.SessionID == sessionID &&
		routingerr.IsCompleteRelativeQuotaNoticeText(turn.quotaNoticeText) {
		return &providerPromptError{ProviderError: turn.pendingQuotaNotice.ProviderError}
	}
	if turn.completedQuotaNotice != nil && turn.completedQuotaNotice.SessionID == sessionID {
		return &providerPromptError{ProviderError: turn.completedQuotaNotice.ProviderError}
	}
	return nil
}

func (turn *promptTurnState) deliverQuotaNotice(a *Adapter, diagnostic providerNoticeDiagnostic, rule string) {
	turn.evidenceMu.Lock()
	defer turn.evidenceMu.Unlock()
	if turn.quotaNoticeDelivered {
		return
	}
	if rule == routingerr.RuleRelativeQuotaNotice &&
		relativeQuotaNoticeContainsOutput(turn.quotaNoticeText, quotaNoticeSentenceComplete(turn.quotaNoticeText)) {
		return
	}
	turn.quotaNoticeDelivered = true
	turn.pendingQuotaNotice = nil
	if rule == routingerr.RuleRelativeQuotaNotice {
		turn.completedQuotaNotice = &diagnostic
	}
	if turn.quotaNoticeTimer != nil {
		turn.quotaNoticeTimer.Stop()
	}
	select {
	case turn.providerErrorCh <- diagnostic:
		a.logger.Info("usage-limit notice stated as an agent message; ending the prompt turn",
			zap.String("session_id", diagnostic.SessionID),
			zap.String("agent_id", a.agentID),
			zap.String("classifier_rule", rule))
	default:
		a.logger.Debug("dropping usage-limit notice because the prompt diagnostic channel is full",
			zap.String("session_id", diagnostic.SessionID))
	}
}

func (a *Adapter) usageLimitNoticeEventAccepted(promptGeneration uint64, event *AgentEvent) bool {
	return a.agentID != "" && event != nil && event.Type == streams.EventTypeMessageChunk &&
		event.Role == "" && event.Text != "" && promptGeneration != 0
}

// A stream may stop temporarily after a valid duration prefix. The quota still
// settles through its typed path, but an unfinished sentence proves no reset
// deadline; later text could extend it with another duration component.
func quotaNoticeResetHint(text string, hint *time.Time) *time.Time {
	if !quotaNoticeSentenceComplete(text) {
		return nil
	}
	return hint
}

func quotaNoticeSentenceComplete(text string) bool {
	text = strings.TrimRight(text, " \t")
	return text != "" && strings.ContainsRune(".!?;\r\n", rune(text[len(text)-1]))
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
