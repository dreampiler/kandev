package lifecycle

import (
	"context"
	"errors"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/task/models"
)

type legacyContinuationBootstrapReader interface {
	HasPreparedLegacyContinuationBootstrap(context.Context, string, string, string) (bool, error)
}

// An authorized continuation may retain setup events before SQL commits its
// successor generation. Replay preserves those events; the normal projection
// path still waits for the committed generation before creating any receipt.
func canReplayContinuationSetupEvents(ctx context.Context, execution *AgentExecution, delivery AgentDeliveryRepository, status *agentctl.DeliveryStatus) bool {
	stream := status.Stream
	if !execution.ForceContextContinuation || stream == nil || stream.FirstRetained != 1 ||
		stream.Acknowledged != 0 || status.Unresolved || status.SubmissionCount != 0 ||
		len(status.Submissions) != 0 || status.SubmissionsTruncated {
		return false
	}
	reader, ok := delivery.(harnessGenerationReader)
	if !ok {
		return false
	}
	current, err := reader.GetCurrentHarnessSessionGeneration(ctx, execution.SessionID, execution.DeliveryIncarnationID)
	if errors.Is(err, models.ErrTaskSessionNotFound) {
		return canReplayLegacyContinuationSetupEvents(ctx, execution, delivery)
	}
	return err == nil && restoreGenerationPending(execution, current)
}

func canReplayLegacyContinuationSetupEvents(ctx context.Context, execution *AgentExecution, delivery AgentDeliveryRepository) bool {
	if execution.DeliveryHarnessGeneration != 1 || !execution.deliveryBootstrap.Load() ||
		execution.SessionID == "" || execution.DeliveryIncarnationID == "" || execution.ID == "" {
		return false
	}
	prepared, ok := delivery.(legacyContinuationBootstrapReader)
	if !ok {
		return false
	}
	valid, err := prepared.HasPreparedLegacyContinuationBootstrap(ctx, execution.SessionID, execution.DeliveryIncarnationID, execution.ID)
	return err == nil && valid
}
