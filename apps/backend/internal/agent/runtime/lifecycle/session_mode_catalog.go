package lifecycle

import "go.uber.org/zap"

// A known catalog can rule out a stale mode without sending it to the provider.
// An unknown catalog keeps the existing application and confirmation path.
func (sm *SessionManager) startupModeUnavailable(execution *AgentExecution, sessionID, requested string) bool {
	if state := execution.GetModelState(); state != nil {
		for _, option := range state.ConfigOptions {
			if option.Type != "select" || option.Category != "mode" {
				continue
			}
			for _, value := range option.Options {
				if value.Value == requested {
					return false
				}
			}
			sm.warnUnavailableStartupMode(execution, sessionID, requested, option.CurrentValue)
			return true
		}
	}
	if state := execution.GetModeState(); state != nil && len(state.AvailableModes) > 0 {
		for _, mode := range state.AvailableModes {
			if mode.ID == requested {
				return false
			}
		}
		sm.warnUnavailableStartupMode(execution, sessionID, requested, state.CurrentModeID)
		return true
	}
	return false
}

func (sm *SessionManager) warnUnavailableStartupMode(execution *AgentExecution, sessionID, requested, effective string) {
	sm.logger.Warn("requested startup mode is not advertised; retaining provider default",
		zap.String("execution_id", execution.ID), zap.String("session_id", sessionID),
		zap.String("requested_mode", requested), zap.String("effective_mode", effective))
}
