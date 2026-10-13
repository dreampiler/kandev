package orchestrator

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

// sweepResidualDeliveryNoOp records guarded workflow no-op receipts for
// block_resolved terminal settlements whose workflow receipt is still
// missing, then leaves promotion to the existing reconcile path. It returns
// the number of receipts recorded.
//
// A block_resolved settlement without its workflow receipt pins the stream's
// suffix forever: promotion requires hasExactAgentDeliveryWorkflowOutcomeTx
// and retention never creates workflow receipts. The sweep closes that gap
// for turns that never reached on_turn_complete evaluation (for example a
// session that ended before its turn completed): it records the guarded
// no-op through the same Task 04 writer the live path uses
// (recordDeliveryWorkflowNoOp), so the next reconcile promotes the terminal
// through the unchanged CompleteAgentDeliveryTerminalSettlement path.
//
// The sweep is idempotent: a recorded receipt is never rewritten, and a
// terminal that gained its receipt by another path is skipped.
func (s *Service) sweepResidualDeliveryNoOp(ctx context.Context) (int, error) {
	settlements, ok := s.repo.(repository.AgentDeliverySettlementRepository)
	if !ok {
		return 0, nil
	}
	effects, err := settlements.ListPendingAgentDeliverySettlements(ctx, "")
	if err != nil {
		return 0, err
	}
	swept := 0
	for _, effect := range effects {
		if effect == nil || effect.State != models.DeliveryEffectBlockResolved {
			continue
		}
		recorded, err := s.recordResidualDeliveryNoOp(ctx, effect)
		if err != nil {
			return swept, err
		}
		if recorded {
			swept++
		}
	}
	return swept, nil
}

// recordResidualDeliveryNoOp records a single guarded no-op receipt for a
// block_resolved terminal that still lacks its workflow outcome. It returns
// false when the terminal already has a receipt, when the turn identity is
// missing (no workflow key exists for an empty turn), or when the owning task
// cannot supply the Task 04 writer's guarded decision.
func (s *Service) recordResidualDeliveryNoOp(ctx context.Context, effect *models.AgentDeliveryEffect) (bool, error) {
	if effect == nil || effect.TurnID == "" {
		return false, nil
	}
	reader, ok := s.repo.(agentDeliveryEffectReader)
	if !ok {
		return false, nil
	}
	workflowKey := workflowEffectKey(effect.TurnID)
	if workflowKey == "" {
		return false, nil
	}
	stored, err := reader.GetAgentDeliveryEffect(ctx, workflowKey)
	if err == nil && stored != nil {
		return false, nil
	}
	if err != nil && !errors.Is(err, repository.ErrAgentDeliveryEffectNotFound) &&
		!errors.Is(err, repoerrors.ErrAgentDeliveryEffectNotFound) {
		return false, err
	}
	sessionRepo, ok := s.repo.(interface {
		GetTaskSession(ctx context.Context, id string) (*models.TaskSession, error)
	})
	if !ok {
		return false, nil
	}
	session, err := sessionRepo.GetTaskSession(ctx, effect.SessionID)
	if err != nil || session == nil {
		return false, nil
	}
	taskRepo, ok := s.repo.(interface {
		GetTask(ctx context.Context, id string) (*models.Task, error)
	})
	if !ok {
		return false, nil
	}
	task, err := taskRepo.GetTask(ctx, session.TaskID)
	if err != nil || task == nil {
		return false, nil
	}
	workflowEffect := &models.AgentDeliveryEffect{
		EffectKey:         workflowKey,
		EffectType:        "workflow.on_turn_complete",
		State:             models.DeliveryEffectCompleted,
		SessionID:         effect.SessionID,
		IncarnationID:     effect.IncarnationID,
		HarnessGeneration: effect.HarnessGeneration,
		StreamID:          effect.StreamID,
		Sequence:          effect.Sequence,
		SubmissionID:      effect.SubmissionID,
		TurnID:            effect.TurnID,
		TaskID:            task.ID,
	}
	committed := s.recordDeliveryWorkflowNoOp(ctx, task, workflowEffect, residualNoOpOutcome(task))
	return committed, nil
}

// residualNoOpOutcome picks the guarded no-op outcome for a swept terminal.
// Sessions without a workflow step never had a transition to evaluate, so
// they record not_required, matching the live no-workflow path.
func residualNoOpOutcome(task *models.Task) string {
	if task == nil || task.WorkflowStepID == "" {
		return "not_required"
	}
	return "no_automatic_transition"
}
