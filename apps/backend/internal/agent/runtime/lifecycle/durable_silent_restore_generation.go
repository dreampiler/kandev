package lifecycle

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// Native restore can emit setup events before its candidate is committed in SQL.
// An interrupted-conversation successor or authorized context continuation may
// wait for that commit. Receipt and projection still require the committed
// generation and live owner.
func awaitSilentRestoreGeneration(
	ctx context.Context, execution *AgentExecution, reader harnessGenerationReader, sessionID, incarnationID string,
) (*models.HarnessSessionGeneration, error) {
	current, err := reader.GetCurrentHarnessSessionGeneration(ctx, sessionID, incarnationID)
	if err != nil || !restoreGenerationPending(execution, current) {
		return current, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		current, err = reader.GetCurrentHarnessSessionGeneration(ctx, sessionID, incarnationID)
		if err != nil || !restoreGenerationPending(execution, current) {
			return current, err
		}
	}

}

func restoreGenerationPending(execution *AgentExecution, current *models.HarnessSessionGeneration) bool {
	if execution != nil && current != nil && execution.ForceContextContinuation {
		return current.SessionID == execution.SessionID && current.IncarnationID == execution.DeliveryIncarnationID &&
			current.Generation > 0 && uint64(current.Generation)+1 == execution.DeliveryHarnessGeneration
	}
	return silentRestoreGenerationPending(execution, current)
}

func silentRestoreGenerationPending(execution *AgentExecution, current *models.HarnessSessionGeneration) bool {
	return execution != nil && current != nil && execution.RequiredNativeConversationID != "" &&
		execution.InterruptedSubmissionID != "" && execution.InterruptedStreamID != "" &&
		execution.InterruptedHarnessGeneration > 0 && execution.InterruptedHarnessGeneration+1 == execution.DeliveryHarnessGeneration &&
		current.SessionID == execution.SessionID && current.IncarnationID == execution.DeliveryIncarnationID &&
		uint64(current.Generation) == execution.InterruptedHarnessGeneration && current.NativeSessionID == execution.RequiredNativeConversationID
}
