package orchestrator

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func (s *Service) prepareInitialAgentDeliverySubmission(
	ctx context.Context,
	session *models.TaskSession,
	executionID, prompt string,
	attachments []v1.MessageAttachment,
	options promptTaskOptions,
) error {
	if options.deliverySubmissionID == "" {
		protocol, err := s.resolveDeliveryProtocol(ctx, session.ID, executionID, options.deliveryProtocol)
		if err != nil {
			return err
		}
		if protocol == messagequeue.DeliveryProtocolLegacy {
			return nil
		}
		incarnationID := session.QueueIncarnationID
		if incarnationID == "" {
			incarnationID = session.ID
		}
		generation, err := s.currentDeliveryGeneration(ctx, session, incarnationID)
		if err != nil {
			return s.deliveryRecoveryError(ctx, session.ID, "harness_generation_unavailable")
		}
		// The initial lifecycle dispatch derives this identity from the same owner and generation.
		options.deliverySubmissionID = fmt.Sprintf("prompt:initial:%s:%d:%s", session.ID, generation, executionID)
		options.deliveryProtocol = protocol
		options.expectedDeliveryGeneration = generation
	}
	_, err := s.prepareAgentDeliverySubmission(ctx, session, executionID, prompt, attachments, options)
	return err
}
