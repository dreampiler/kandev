package orchestrator

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"go.uber.org/zap"
)

// reconcileOrphanedSessionTransfersOnce shares the idle-reaper tick and the
// startup recovery decision. A live transfer remains fenced until its owner
// completes it; periodic recovery only claims terminal, delivery-free endpoints.
func (s *Service) reconcileOrphanedSessionTransfersOnce(ctx context.Context) {
	if s.messageQueue == nil || !s.messageQueue.SessionTransferCompensationPersistenceAvailable() {
		return
	}
	compensations, err := s.messageQueue.ListSessionTransferCompensations(ctx)
	if err != nil {
		s.logger.Warn("session transfer sweep: list compensations failed", zap.Error(err))
		return
	}
	for _, compensation := range compensations {
		if ctx.Err() != nil {
			return
		}
		if len(compensation.AttachmentIDs) > 0 && !sessionAttachmentTransfererAvailable(s.sessionAttachmentTransferer) {
			s.logger.Warn("session transfer sweep: attachment transfer service unavailable",
				zap.String("task_id", compensation.TaskID))
			continue
		}
		ownerID, claimErr := s.messageQueue.ClaimOrphanedSessionTransferCompensationRecovery(ctx, compensation)
		if errors.Is(claimErr, messagequeue.ErrSessionTransferInProgress) {
			continue
		}
		if claimErr == nil {
			claimErr = s.recoverClaimedSessionTransferCompensation(ctx, compensation, ownerID)
		}
		if claimErr != nil {
			s.logger.Warn("session transfer sweep: recovery deferred",
				zap.String("task_id", compensation.TaskID), zap.Error(claimErr))
		}
	}
}
