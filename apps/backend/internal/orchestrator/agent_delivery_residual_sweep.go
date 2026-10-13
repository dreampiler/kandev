package orchestrator

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

// sweepResidualDeliveryNoOp records guarded workflow no-op receipts for
// block_resolved terminal settlements whose workflow receipt is still
// missing, then leaves promotion to the existing reconcile path. It returns
// the number of receipts recorded. sessionID == "" scans every session.
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
func (s *Service) sweepResidualDeliveryNoOp(ctx context.Context, sessionID string) (int, error) {
	settlements, ok := s.repo.(repository.AgentDeliverySettlementRepository)
	if !ok {
		return 0, nil
	}
	effects, err := settlements.ListPendingAgentDeliverySettlements(ctx, sessionID)
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
	recorded, err := residualNoOpReceiptExists(ctx, reader, workflowKey)
	if err != nil || recorded {
		return false, err
	}
	task, err := s.residualNoOpTask(ctx, effect.SessionID)
	if err != nil || task == nil {
		return false, err
	}
	outcome := residualNoOpOutcome(task)
	if outcome == "" {
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
	return s.recordDeliveryWorkflowNoOp(ctx, task, workflowEffect, outcome), nil
}

// residualNoOpReceiptExists reports whether an exact workflow receipt already
// exists for the turn; a missing receipt is not an error.
func residualNoOpReceiptExists(
	ctx context.Context,
	reader agentDeliveryEffectReader,
	workflowKey string,
) (bool, error) {
	stored, err := reader.GetAgentDeliveryEffect(ctx, workflowKey)
	if err == nil {
		return stored != nil, nil
	}
	if errors.Is(err, repository.ErrAgentDeliveryEffectNotFound) ||
		errors.Is(err, repoerrors.ErrAgentDeliveryEffectNotFound) {
		return false, nil
	}
	return false, err
}

// residualNoOpTask resolves the owning task for a session. It returns (nil,
// nil) when the session or task is unavailable, so the caller simply skips.
func (s *Service) residualNoOpTask(ctx context.Context, sessionID string) (*models.Task, error) {
	sessionRepo, ok := s.repo.(interface {
		GetTaskSession(ctx context.Context, id string) (*models.TaskSession, error)
	})
	if !ok {
		return nil, nil
	}
	session, err := sessionRepo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil {
		return nil, nil
	}
	taskRepo, ok := s.repo.(interface {
		GetTask(ctx context.Context, id string) (*models.Task, error)
	})
	if !ok {
		return nil, nil
	}
	task, err := taskRepo.GetTask(ctx, session.TaskID)
	if err != nil || task == nil {
		return nil, nil
	}
	return task, nil
}

// residualNoOpOutcome returns the only guarded no-op the sweep may record
// without an on_turn_complete evaluation: not_required for a terminal whose
// task has no workflow step at all, so there is genuinely nothing to
// transition. A task that does have a workflow step may still need a real
// automatic transition, so the sweep returns "" and leaves that terminal
// pinned rather than fabricating no_automatic_transition to unpin it.
func residualNoOpOutcome(task *models.Task) string {
	if task == nil || task.WorkflowStepID == "" {
		return "not_required"
	}
	return ""
}

// settleDeadOwnerDeliverySubmissions settles active submissions whose owner
// session is dead, through the existing terminal settlement path. It scans a
// bounded work queue (sessionID == "" scans every session) and re-checks every
// guard inside SettleDeadOwnerAgentDeliverySubmission, so it never settles a
// submission with a live owner, an open recovery block, or pending queue,
// dispatch, or send-now evidence.
func (s *Service) settleDeadOwnerDeliverySubmissions(ctx context.Context, sessionID string) (int, error) {
	repo, ok := s.repo.(interface {
		ListActiveAgentDeliverySubmissionIDs(context.Context, string, int) ([]string, error)
		SettleDeadOwnerAgentDeliverySubmission(context.Context, string) (bool, error)
	})
	if !ok {
		return 0, nil
	}
	ids, err := repo.ListActiveAgentDeliverySubmissionIDs(ctx, sessionID, 0)
	if err != nil {
		return 0, err
	}
	settled := 0
	for _, id := range ids {
		committed, err := repo.SettleDeadOwnerAgentDeliverySubmission(ctx, id)
		if err != nil {
			return settled, err
		}
		if committed {
			settled++
		}
	}
	return settled, nil
}

// reconcileResidualDeliverySettlements runs the two residual-settlement passes
// for one session (sessionID == "" means every session) from the existing
// startup, reconcile, and session-terminal entry points. Both passes are
// bounded, idempotent, and reuse the existing settlement paths; a failure is
// logged and never propagated, so it can not break the transition that
// triggered it.
func (s *Service) reconcileResidualDeliverySettlements(ctx context.Context, sessionID string) {
	if _, err := s.settleDeadOwnerDeliverySubmissions(ctx, sessionID); err != nil {
		s.logger.Warn("failed to settle dead-owner delivery submissions",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}
	if _, err := s.sweepResidualDeliveryNoOp(ctx, sessionID); err != nil {
		s.logger.Warn("failed to sweep residual delivery workflow no-ops",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}
}
