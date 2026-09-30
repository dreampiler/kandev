package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/events/bus"
)

// subscribeWorkflowQueueEvents keeps queue admission in sync with workflow
// configuration changes. This also covers changing a step from a bounded WIP
// limit to unlimited, or changing its feeder.
func (s *Service) subscribeWorkflowQueueEvents() {
	if s.eventBus == nil {
		return
	}
	if _, err := s.eventBus.Subscribe(events.WorkflowStepUpdated, s.handleWorkflowStepQueueUpdate); err != nil {
		s.logger.Error("failed to subscribe to workflow step updates for queue reconciliation", zap.Error(err))
	}
}

func (s *Service) handleWorkflowStepQueueUpdate(ctx context.Context, event *bus.Event) error {
	if s.workflowStore == nil || event == nil {
		return nil
	}
	data, ok := event.Data.(map[string]interface{})
	if !ok {
		return nil
	}
	step, ok := data["step"].(map[string]interface{})
	if !ok {
		return nil
	}
	stepID, _ := step["id"].(string)
	if stepID != "" {
		s.workflowStore.pullNextTaskOnVacate(ctx, stepID, "")
	}
	return nil
}

// queuePromotionEntryID returns the ledger identity of the step entry that a
// queue promotion delivers. A task queued on a full step entered that step when
// it was queued, so the same-step write that later admits it records no
// transition and its delivery carries no id. The entry being dispatched is then
// the task's latest ledger row, which lets the entry guard tell this current
// entry apart from a late callback for an earlier step.
func (s *Service) queuePromotionEntryID(ctx context.Context, taskID string, deliveredID int64) int64 {
	if deliveredID > 0 {
		return deliveredID
	}
	reader, ok := s.repo.(workflowStepTransitionReader)
	if !ok {
		return deliveredID
	}
	latestID, err := reader.GetLatestTaskStepTransitionID(ctx, taskID)
	if err != nil {
		s.logger.Warn("task.queue_promoted: failed to read the promoted step entry",
			zap.String("task_id", taskID), zap.Error(err))
		return deliveredID
	}
	return latestID
}
