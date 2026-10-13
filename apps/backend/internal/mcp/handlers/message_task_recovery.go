package handlers

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/orchestrator"
)

type blockedTaskMessageRecoverer interface {
	AutoRecoverBlockedPrompt(context.Context, string, string, error) bool
}

func (h *Handlers) autoRecoverTaskMessagePrompt(ctx context.Context, taskID, sessionID string, cause error) bool {
	if !errors.Is(cause, orchestrator.ErrSessionRecoveryRequired) {
		return false
	}
	recoverer, ok := h.sessionLauncher.(blockedTaskMessageRecoverer)
	return ok && recoverer.AutoRecoverBlockedPrompt(ctx, taskID, sessionID, cause)
}
