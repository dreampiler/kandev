package orchestrator

import (
	"context"
	"database/sql"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// Agent-delivery workflow outcomes the residual sweep can record through the
// guarded no-op writer. The values mirror the closed set
// hasExactAgentDeliveryWorkflowOutcomeTx promotes on.
const (
	agentDeliveryOutcomeNotRequired = "not_required"
	agentDeliveryOutcomeNoAutomatic = "no_automatic_transition"
)

// sweepResidualDeliveryNoOp records guarded workflow no-op receipts for
// block_resolved terminal settlements whose workflow receipt is still
// missing, then leaves promotion to the existing reconcile path. It returns
// the number of receipts recorded and the number of current-step terminals it
// deliberately left pinned for a real transition. sessionID == "" scans every
// session.
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
func (s *Service) sweepResidualDeliveryNoOp(ctx context.Context, sessionID string) (int, int, error) {
	settlements, ok := s.repo.(repository.AgentDeliverySettlementRepository)
	if !ok {
		return 0, 0, nil
	}
	effects, err := settlements.ListPendingAgentDeliverySettlements(ctx, sessionID)
	if err != nil {
		return 0, 0, err
	}
	swept, pinned := 0, 0
	for _, effect := range effects {
		if effect == nil || effect.State != models.DeliveryEffectBlockResolved {
			continue
		}
		recorded, protected, err := s.recordResidualDeliveryNoOp(ctx, effect)
		if err != nil {
			return swept, pinned, err
		}
		if recorded {
			swept++
		}
		if protected {
			pinned++
		}
	}
	if pinned > 0 {
		s.logger.Info("residual delivery sweep left current-step terminals pinned for a real transition",
			zap.String("session_id", sessionID), zap.Int("pinned", pinned))
	}
	return swept, pinned, nil
}

// recordResidualDeliveryNoOp records a single guarded no-op receipt for a
// block_resolved terminal that still lacks its workflow outcome. It returns
// false when the terminal already has a receipt, when the turn identity is
// missing (no workflow key exists for an empty turn), or when the owning task
// cannot supply the Task 04 writer's guarded decision. pinned is true only
// when the terminal is a current-step turn whose step declares an automatic
// move, so the sweep must leave it for a real transition.
func (s *Service) recordResidualDeliveryNoOp(ctx context.Context, effect *models.AgentDeliveryEffect) (recorded bool, pinned bool, err error) {
	if effect == nil || effect.TurnID == "" {
		return false, false, nil
	}
	reader, ok := s.repo.(agentDeliveryEffectReader)
	if !ok {
		return false, false, nil
	}
	workflowKey := workflowEffectKey(effect.TurnID)
	if workflowKey == "" {
		return false, false, nil
	}
	alreadyRecorded, err := residualNoOpReceiptExists(ctx, reader, workflowKey)
	if err != nil || alreadyRecorded {
		return false, false, err
	}
	task, err := s.residualNoOpTask(ctx, effect.SessionID)
	if err != nil || task == nil {
		return false, false, err
	}
	outcome, protected := s.residualDeliveryOutcome(ctx, task, effect)
	if outcome == "" {
		return false, protected, nil
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
	return s.recordDeliveryWorkflowNoOp(ctx, task, workflowEffect, outcome), false, nil
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

// residualDeliveryOutcome decides the guarded workflow outcome the residual
// sweep may record for a block_resolved terminal whose workflow receipt is
// missing. It reasons only from durable facts, never calls the workflow
// engine, never moves a step, and never publishes.
//
// A task with no workflow step has nothing to transition, so the outcome is
// not_required. Otherwise the terminal's turn supplies the entry evidence: a
// turn stamped in a step the task has since left was replaced by that step
// move, so its completion can no longer drive an entry and is not_required.
// A current-step turn whose step declares no automatic on_turn_complete move
// deterministically yields no_automatic_transition. A current-step turn whose
// step does declare an automatic move is left protected (outcome "" and
// pinned true): a real transition must not be fabricated retroactively.
func (s *Service) residualDeliveryOutcome(
	ctx context.Context,
	task *models.Task,
	effect *models.AgentDeliveryEffect,
) (outcome string, pinned bool) {
	if task == nil {
		return "", false
	}
	if task.WorkflowStepID == "" {
		return agentDeliveryOutcomeNotRequired, false
	}
	turn, err := s.residualTurn(ctx, effect.TurnID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		s.logger.Warn("residual delivery sweep could not read the terminal turn",
			zap.String("turn_id", effect.TurnID), zap.Error(err))
		return "", false
	}
	if turn == nil {
		return "", false
	}
	// A turn without a step-entry stamp carries no entry evidence: it may have
	// started before the task held a step, so it stays protected.
	stamp := models.StringFromAny(turn.Metadata[models.TurnMetaKeyWorkflowStepIDAtStart])
	if stamp == "" {
		return "", false
	}
	if stamp != task.WorkflowStepID {
		return agentDeliveryOutcomeNotRequired, false
	}
	if s.workflowStepGetter == nil {
		return "", false
	}
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if errors.Is(err, wfmodels.ErrWorkflowStepNotFound) {
		return "", false
	}
	if err != nil {
		s.logger.Warn("residual delivery sweep could not read the current workflow step",
			zap.String("task_id", task.ID), zap.String("step_id", task.WorkflowStepID), zap.Error(err))
		return "", false
	}
	if step == nil || step.WorkflowID != task.WorkflowID {
		return "", false
	}
	if step.AdvancesOnTurnComplete() {
		return "", true
	}
	return agentDeliveryOutcomeNoAutomatic, false
}

// residualTurn reads the turn row that carries the step-entry stamp. A missing
// turn is not an error: the caller leaves the terminal protected because it
// lacks the entry and turn evidence a decision requires.
func (s *Service) residualTurn(ctx context.Context, turnID string) (*models.Turn, error) {
	reader, ok := s.repo.(interface {
		GetTurn(ctx context.Context, id string) (*models.Turn, error)
	})
	if !ok {
		return nil, nil
	}
	return reader.GetTurn(ctx, turnID)
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
	if _, _, err := s.sweepResidualDeliveryNoOp(ctx, sessionID); err != nil {
		s.logger.Warn("failed to sweep residual delivery workflow no-ops",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}
}
