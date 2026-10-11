package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

const (
	// agentDeliveryRetentionGrace is how long a projected delivery payload is
	// retained before it may be removed. It keeps recent replay and diagnostics
	// available without retaining a permanent row per streamed event.
	agentDeliveryRetentionGrace   = 1 * time.Hour
	agentDeliveryRetentionStreams = 25
	agentDeliveryRetentionRows    = 200
)

// agentDeliveryRetentionRepository is the narrow retirement capability off the
// task repository. It is type-asserted rather than added to
// repository.TaskRepository so that repository's many test doubles do not have
// to grow a method this sweep never exercises through them.
type agentDeliveryRetentionRepository interface {
	RetireAgentDeliveryPrefix(
		ctx context.Context,
		grace time.Duration,
		now time.Time,
		budget models.AgentDeliveryRetirementBudget,
	) (models.AgentDeliveryRetirementResult, error)
}

// runAgentDeliveryRetentionSweep removes one bounded slice of each eligible
// stream's contiguous projected-and-settled delivery prefix on every tick. It
// bounds inbox and generic-receipt growth from long-lived streams while
// preserving unprojected rows and unsettled submissions. A missing capability
// or an error ends the pass without affecting the other reconciliation passes.
func (s *Service) runAgentDeliveryRetentionSweep(ctx context.Context, now time.Time) {
	repo, ok := s.tasks.(agentDeliveryRetentionRepository)
	if !ok {
		return
	}
	result, err := repo.RetireAgentDeliveryPrefix(
		ctx,
		agentDeliveryRetentionGrace,
		now,
		models.AgentDeliveryRetirementBudget{
			Streams: agentDeliveryRetentionStreams,
			Rows:    agentDeliveryRetentionRows,
		},
	)
	if err != nil {
		s.logger.Warn("agent delivery retention: retirement pass failed", zap.Error(err))
		return
	}
	if result.RowsRemoved > 0 {
		s.logger.Info("agent delivery retention: retired projected delivery rows",
			zap.Int("streams_visited", result.StreamsVisited),
			zap.Int64("rows_removed", result.RowsRemoved),
			zap.Int64("bytes_removed", result.BytesRemoved))
	}
}
