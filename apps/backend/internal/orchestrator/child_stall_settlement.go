package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// markChildTurnCancelled records an explicit user cancellation on a child
// task's captured turn, including a completion that has not been classified.
func (s *Service) markChildTurnCancelled(ctx context.Context, sessionID string, prepared cancelAgentPreparation) {
	turnID := prepared.capturedTurnID
	if turnID == "" {
		turnID = prepared.cancelTurnID
	}
	if turnID == "" || s.turnService == nil {
		return
	}
	turn, err := s.turnService.GetTurn(ctx, turnID)
	if err != nil || turn == nil {
		return
	}
	if _, classified := models.LoadChildStallState(turn.Metadata); turn.CompletedAt != nil && classified {
		return
	}
	if _, isChild := models.LoadChildStallStart(turn.Metadata); !isChild {
		return
	}
	if err := s.turnService.PatchTurnMetadata(ctx, sessionID, turnID, map[string]interface{}{
		models.TurnMetaKeyChildStallSettlement: models.ChildStallSettlementCancelled,
	}); err != nil {
		s.logger.Debug("failed to record child turn cancellation",
			zap.String("session_id", sessionID), zap.String("turn_id", turnID), zap.Error(err))
	}
}
