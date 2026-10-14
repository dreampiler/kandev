package maintenance

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

const offlineDeliveryEventPassTimeout = 2 * time.Minute

// Each batch commits its own bounded transaction. Continue through protected
// discovery pages while the persisted frontier advances; two idle batches
// allow both alternating discovery phases to exhaust without a busy loop.
func executeDeliveryEventBatches(
	ctx context.Context,
	repo deliveryRetentionRepository,
	options models.AgentDeliveryRetentionBatchOptions,
	drain bool,
) (ExecutionResult, error) {
	passCtx, cancel := context.WithTimeout(ctx, offlineDeliveryEventPassTimeout)
	defer cancel()
	var execution ExecutionResult
	idle := 0
	for {
		result, err := repo.RunAgentDeliveryRetentionBatch(passCtx, options)
		if err != nil {
			if !isTimeout(err) {
				return execution, err
			}
			execution.Partial = true
			return execution, nil
		}
		execution.DeliveryBatches++
		execution.DeletedDeliveryEventPayloads += result.EventPayloadsPruned
		execution.DeletedSettledDeliveryPrompts += result.PromptPayloadsPruned
		execution.RetiredDeliveryEventReceipts += result.EventRowsRetired
		if !drain {
			execution.Partial = result.Candidates >= deliveryCandidateLimit(options.MaxCandidates)
			return execution, nil
		}
		if result.Candidates == 0 {
			return execution, nil
		}
		changed := result.EventPayloadsPruned + result.PromptPayloadsPruned + result.EventRowsRetired
		if result.Candidates > 0 && (changed > 0 || result.DiscoveryAdvanced) {
			idle = 0
		} else {
			idle++
		}
		if idle >= 2 {
			return execution, nil
		}
		if passCtx.Err() != nil {
			execution.Partial = true
			return execution, nil
		}
	}
}
