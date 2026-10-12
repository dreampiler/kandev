package lifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

const (
	silentRestoreGenerationWait = 30 * time.Second
	// A context continuation's generation is committed only after its whole
	// launch returns (session/new, settings, boot readiness and the startup
	// workspace refresh), which production measured beyond 30s.
	contextContinuationGenerationWait = 3 * time.Minute
)

// errSuccessorGenerationDetached ends a successor wait whose updates stream
// already closed: that launch can no longer commit the generation.
var errSuccessorGenerationDetached = errors.New("successor delivery stream detached before its generation commit")

type deliveryDetachKey struct{}

// withDeliveryDetachSignal lets a stream processor end successor waits when its
// transport closes, without cancelling the context used to drain queued events.
func withDeliveryDetachSignal(ctx context.Context, detached <-chan struct{}) context.Context {
	return context.WithValue(ctx, deliveryDetachKey{}, detached)
}

func deliveryDetachSignal(ctx context.Context) <-chan struct{} {
	detached, _ := ctx.Value(deliveryDetachKey{}).(<-chan struct{})
	return detached
}

// Native restore can emit setup events before its candidate is committed in SQL.
// Only an explicit successor (an interrupted-conversation restore, or an
// explicit context continuation whose generation the orchestrator commits after
// the launch returns) may wait for that commit; receipt and projection still
// require the committed generation and live owner.
func awaitSilentRestoreGeneration(
	ctx context.Context, execution *AgentExecution, reader harnessGenerationReader, sessionID, incarnationID string,
) (*models.HarnessSessionGeneration, error) {
	current, err := reader.GetCurrentHarnessSessionGeneration(ctx, sessionID, incarnationID)
	if err != nil || !successorGenerationPending(execution, current) {
		return current, err
	}
	wait := silentRestoreGenerationWait
	if contextContinuationGenerationPending(execution, current) {
		wait = contextContinuationGenerationWait
	}
	detached := deliveryDetachSignal(ctx)
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-detached:
			timer.Stop()
			return nil, errSuccessorGenerationDetached
		case <-timer.C:
		}
		current, err = reader.GetCurrentHarnessSessionGeneration(ctx, sessionID, incarnationID)
		if err != nil || !successorGenerationPending(execution, current) {
			return current, err
		}
	}

}

func silentRestoreGenerationPending(execution *AgentExecution, current *models.HarnessSessionGeneration) bool {
	return execution != nil && current != nil && execution.RequiredNativeConversationID != "" &&
		execution.InterruptedSubmissionID != "" && execution.InterruptedStreamID != "" &&
		execution.InterruptedHarnessGeneration > 0 && execution.InterruptedHarnessGeneration+1 == execution.DeliveryHarnessGeneration &&
		current.SessionID == execution.SessionID && current.IncarnationID == execution.DeliveryIncarnationID &&
		uint64(current.Generation) == execution.InterruptedHarnessGeneration && current.NativeSessionID == execution.RequiredNativeConversationID
}

func successorGenerationPending(execution *AgentExecution, current *models.HarnessSessionGeneration) bool {
	return silentRestoreGenerationPending(execution, current) || contextContinuationGenerationPending(execution, current)
}

// An explicit context continuation launches its new native conversation with
// generation N+1 while SQL still names generation N; the orchestrator commits
// N+1 only after the launch returns. Its setup events must wait for that
// commit instead of failing the owner check against the predecessor.
func contextContinuationGenerationPending(execution *AgentExecution, current *models.HarnessSessionGeneration) bool {
	return execution != nil && current != nil && execution.ForceContextContinuation &&
		current.Generation > 0 && uint64(current.Generation)+1 == execution.DeliveryHarnessGeneration &&
		current.SessionID == execution.SessionID && current.IncarnationID == execution.DeliveryIncarnationID
}
