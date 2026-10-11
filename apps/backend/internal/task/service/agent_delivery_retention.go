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
	agentDeliveryRetentionGrace          = 1 * time.Hour
	agentDeliveryRetentionStreams        = 25
	agentDeliveryRetentionRowsPerBatch   = 200
	agentDeliveryRetentionPassBudgetBase = 5 * time.Second
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

// runAgentDeliveryRetentionSweep drains each eligible stream's contiguous
// projected-and-settled delivery prefix in bounded batches until no more work
// remains or the pass budget expires. It bounds inbox and generic-receipt
// growth from long-lived streams while preserving unprojected rows and
// unsettled submissions. A missing capability or an error ends the pass.
func (s *Service) runAgentDeliveryRetentionSweep(ctx context.Context, now time.Time) {
	repo, ok := s.tasks.(agentDeliveryRetentionRepository)
	if !ok {
		return
	}
	deadline := now.Add(agentDeliveryRetentionPassBudgetBase)
	var streams int
	var rows, bytes int64
	for {
		result, err := repo.RetireAgentDeliveryPrefix(
			ctx,
			agentDeliveryRetentionGrace,
			time.Now().UTC(),
			models.AgentDeliveryRetirementBudget{
				Streams: agentDeliveryRetentionStreams,
				Rows:    agentDeliveryRetentionRowsPerBatch,
			},
		)
		if err != nil {
			s.logger.Warn("agent delivery retention: retirement pass failed", zap.Error(err))
			break
		}
		streams += result.StreamsVisited
		rows += result.RowsRemoved
		bytes += result.BytesRemoved
		if result.RowsRemoved == 0 || time.Now().UTC().After(deadline) {
			break
		}
	}
	if rows > 0 {
		s.logger.Info("agent delivery retention: retired projected delivery rows",
			zap.Int("streams_visited", streams),
			zap.Int64("rows_removed", rows),
			zap.Int64("bytes_removed", bytes))
	}
}
