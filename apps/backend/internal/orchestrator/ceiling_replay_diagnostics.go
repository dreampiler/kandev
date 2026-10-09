package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

func (s *Service) logCeilingReplayPause(ctx context.Context, taskID string, deferral models.CeilingDeferral, reason, detail string) {
	s.logger.Zap().Info("ceiling replay paused before admission",
		zap.String("task_id", taskID),
		zap.String("session_id", sessionIDFromCeilingPayload(deferral)),
		zap.String("kind", string(deferral.Kind)),
		zap.String("reason", reason), zap.String("detail", detail),
		zap.Bool("periodic", isPeriodicCeilingSweep(ctx)))
}

func (s *Service) validateClaimedCeilingReplay(ctx context.Context, taskID string, claim *ceilingDeferredLaunchClaim) *models.Task {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil || task == nil {
		s.logCeilingReplayPause(ctx, taskID, claim.deferral, "task_unavailable", "claimed task could not be read")
		return nil
	}
	disposition, detail, validationErr := s.validateCeilingEntry(ctx, task, claim.deferral)
	if disposition == ceilingEntryValid && validationErr == nil {
		return task
	}
	if disposition == ceilingEntrySuperseded || disposition == ceilingEntryPermanentlyUnavailable {
		s.dropCeilingDeferral(ctx, task, sessionIDFromCeilingPayload(claim.deferral), claim.deferral,
			ceilingReasonSuperseded, detail)
	} else {
		s.logCeilingReplayPause(ctx, taskID, claim.deferral, "entry_unavailable", detail)
	}
	return nil
}
