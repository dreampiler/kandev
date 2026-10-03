package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/childstall"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	"go.uber.org/zap"
)

var errChildStallQueueUnavailable = errors.New("message queue is not configured")

// childStallStaleError means the candidate stopped qualifying at the
// admission boundary.
type childStallStaleError struct{ decision childstall.Decision }

func (e *childStallStaleError) Error() string {
	return "child stall candidate is no longer eligible: " + string(e.decision.Outcome) + " " + e.decision.Reason
}

// childStallAlert is one alert's queue payload.
type childStallAlert struct {
	key      string
	content  string
	metadata map[string]interface{}
}

func (p *ChildStallProducer) deliver(
	ctx context.Context,
	turn *models.Turn,
	start models.ChildStallStart,
	state models.ChildStallState,
	decision childstall.Decision,
	now time.Time,
) {
	parentTask, err := p.store.GetTask(ctx, start.ParentTaskID)
	if err != nil || parentTask == nil || parentTask.ArchivedAt != nil {
		if err != nil && !isTaskNotFound(err) {
			p.retryLater(ctx, turn, state, decision, err, now)
			return
		}
		state.State, state.Reason = string(childstall.OutcomeSuppressed), "parent_unavailable"
		p.persist(ctx, turn, state, true, now)
		recordChildStallReason(state.Reason)
		return
	}
	parentSession, unavailable, err := p.resolveParentSession(ctx, start.ParentTaskID)
	if err != nil {
		p.retryLater(ctx, turn, state, decision, err, now)
		return
	}
	if unavailable {
		p.waitForParent(ctx, turn, start, state, decision, now)
		return
	}
	alert, err := p.buildAlert(ctx, turn, start, decision)
	if err != nil {
		p.retryLater(ctx, turn, state, decision, err, now)
		return
	}
	queued, identity, err := p.admitAlert(ctx, turn, start, parentSession, alert)
	var stale *childStallStaleError
	switch {
	case errors.As(err, &stale):
		state.State, state.Reason = string(stale.decision.Outcome), stale.decision.Reason
		p.persist(ctx, turn, state, stale.decision.Outcome != childstall.OutcomeHeld, now)
		recordChildStallReason(stale.decision.Reason)
		return
	case err != nil:
		p.retryLater(ctx, turn, state, decision, err, now)
		return
	}
	p.markDelivered(ctx, turn, state, decision, queued, parentSession.ID, now)
	p.svc.publishQueueStatusEventForIdentity(ctx, identity)
	p.svc.CheckQueueAdmissionReadiness(ctx, identity)
}

func (p *ChildStallProducer) markDelivered(
	ctx context.Context,
	turn *models.Turn,
	state models.ChildStallState,
	decision childstall.Decision,
	queued *messagequeue.QueuedMessage,
	parentSessionID string,
	now time.Time,
) {
	state.State, state.Cause, state.QuestionID = childStallStateDelivered, string(decision.Cause), decision.QuestionID
	state.ParentSessionID, state.NextAttemptAt, state.LastError = parentSessionID, nil, ""
	if queued != nil {
		state.QueueID = queued.ID
	}
	p.persist(ctx, turn, state, true, now)
	recordChildStallOutcome(childStallStateDelivered)
	recordChildStallCause(string(decision.Cause))
	lag := time.Duration(0)
	if turn.CompletedAt != nil {
		lag = now.Sub(*turn.CompletedAt)
	}
	p.log.Info("queued child stall alert for parent",
		zap.String("child_task_id", turn.TaskID),
		zap.String("turn_id", turn.ID),
		zap.String("parent_session_id", parentSessionID),
		zap.String("queue_id", state.QueueID),
		zap.String("cause", string(decision.Cause)),
		zap.Duration("settlement_to_queue", lag))
}

// waitForParent keeps a qualified candidate until the parent has a promptable
// primary session, notifying the operator once.
func (p *ChildStallProducer) waitForParent(
	ctx context.Context,
	turn *models.Turn,
	start models.ChildStallStart,
	state models.ChildStallState,
	decision childstall.Decision,
	now time.Time,
) {
	first := state.State != childStallStateWaitingParent
	state.State, state.Cause, state.QuestionID = childStallStateWaitingParent, string(decision.Cause), decision.QuestionID
	notify := !state.OperatorNotified
	state.OperatorNotified = true
	if !first && !notify {
		return
	}
	if !p.persist(ctx, turn, state, false, now) {
		return
	}
	recordChildStallOutcome(childStallStateWaitingParent)
	if notify {
		p.notifyOperator(ctx, turn, start, decision)
	}
}

func (p *ChildStallProducer) notifyOperator(
	ctx context.Context,
	turn *models.Turn,
	start models.ChildStallStart,
	decision childstall.Decision,
) {
	recordChildStallOutcome("operator_notified")
	p.log.Warn("child stalled but its parent cannot receive the alert",
		zap.String("child_task_id", turn.TaskID),
		zap.String("parent_task_id", start.ParentTaskID),
		zap.String("cause", string(decision.Cause)))
	if p.svc == nil || p.svc.eventBus == nil {
		return
	}
	_ = p.svc.eventBus.Publish(ctx, events.TaskChildStallUndeliverable, bus.NewEvent(
		events.TaskChildStallUndeliverable, "orchestrator", map[string]interface{}{
			"task_id":        turn.TaskID,
			"session_id":     turn.TaskSessionID,
			"turn_id":        turn.ID,
			"parent_task_id": start.ParentTaskID,
			"cause":          string(decision.Cause),
			"occurrence_id":  childStallKey(turn, start),
		}))
}

// admitAlert rechecks eligibility and admits the keyed alert under the parent
// session's queue admission lock.
func (p *ChildStallProducer) admitAlert(
	ctx context.Context,
	turn *models.Turn,
	start models.ChildStallStart,
	parentSession *models.TaskSession,
	alert childStallAlert,
) (*messagequeue.QueuedMessage, messagequeue.QueueSessionIdentity, error) {
	queue := p.messageQueue()
	if queue == nil {
		return nil, messagequeue.QueueSessionIdentity{}, errChildStallQueueUnavailable
	}
	identity, err := queue.ResolveSessionIdentity(ctx, parentSession.TaskID, parentSession.ID)
	if err != nil {
		return nil, identity, err
	}
	var queued *messagequeue.QueuedMessage
	err = queue.WithSessionAdmission(ctx, parentSession.ID, func(admittedCtx context.Context) error {
		if decision := p.recheck(admittedCtx, turn, start); decision.Outcome != childstall.OutcomeQualified {
			return &childStallStaleError{decision: decision}
		}
		var admitErr error
		queued, _, admitErr = queue.QueueMessageWithMetadataForSessionWithClientQueueID(
			admittedCtx, identity, alert.key, alert.content, "", messagequeue.QueuedByServer,
			false, nil, alert.metadata, nil,
		)
		if errors.Is(admitErr, messagequeue.ErrQueueIDConflict) {
			// The keyed alert was already admitted with different display
			// text (for example a renamed child); the earlier item stands.
			queued = &messagequeue.QueuedMessage{ID: alert.key}
			return nil
		}
		return admitErr
	})
	return queued, identity, err
}

// recheck re-reads the candidate's evidence at a delivery boundary.
func (p *ChildStallProducer) recheck(ctx context.Context, turn *models.Turn, start models.ChildStallStart) childstall.Decision {
	now := p.now()
	evidence, err := p.gatherEvidence(ctx, turn, start, now)
	if err != nil {
		return childstall.Decision{Outcome: childstall.OutcomeSettling}
	}
	decision := childstall.Classify(evidence, p.policy)
	if decision.Outcome == childstall.OutcomeHeld {
		decision = p.promoteHeld(ctx, decision, now)
	}
	return decision
}

// childStallKey is the caller-owned queue admission identity derived from
// (child task, session, turn, start entry).
func childStallKey(turn *models.Turn, start models.ChildStallStart) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d",
		turn.TaskID, turn.TaskSessionID, turn.ID, start.TransitionID)))
	return "child-stall-" + hex.EncodeToString(sum[:20])
}

func (p *ChildStallProducer) buildAlert(
	ctx context.Context,
	turn *models.Turn,
	start models.ChildStallStart,
	decision childstall.Decision,
) (childStallAlert, error) {
	child, err := p.store.GetTask(ctx, turn.TaskID)
	if err != nil {
		return childStallAlert{}, err
	}
	stepName := ""
	if p.svc != nil && p.svc.workflowStepGetter != nil && child.WorkflowStepID != "" {
		if step, stepErr := p.svc.workflowStepGetter.GetStep(ctx, child.WorkflowStepID); stepErr == nil && step != nil {
			stepName = step.Name
		}
	}
	descriptor := map[string]interface{}{
		"child_task_id":    child.ID,
		"child_task_title": child.Title,
		"child_session_id": turn.TaskSessionID,
		"turn_id":          turn.ID,
		"step_id":          child.WorkflowStepID,
		"step_name":        stepName,
		"cause":            string(decision.Cause),
	}
	if decision.QuestionID != "" {
		descriptor["question_id"] = decision.QuestionID
	}
	metadata := map[string]interface{}{
		messagequeue.MetadataChildStallAlert:  true,
		messagequeue.MetadataChildStallAlerts: []interface{}{descriptor},
		messagequeue.MetadataSenderTaskID:     child.ID,
		"sender_task_title":                   child.Title,
		"sender_session_id":                   turn.TaskSessionID,
	}
	return childStallAlert{
		key:      childStallKey(turn, start),
		content:  formatChildStallPrompt(child, turn, stepName, decision),
		metadata: metadata,
	}, nil
}

func formatChildStallPrompt(child *models.Task, turn *models.Turn, stepName string, decision childstall.Decision) string {
	var b strings.Builder
	b.WriteString("CHILD TASK NEEDS ATTENTION\n\n")
	fmt.Fprintf(&b, "Child task: %s (%s)\n", child.Title, child.ID)
	if stepName != "" {
		fmt.Fprintf(&b, "Workflow step: %s\n", stepName)
	}
	fmt.Fprintf(&b, "Cause: %s\n", decision.Cause)
	fmt.Fprintf(&b, "Child turn: %s\n\n", turn.ID)
	b.WriteString(childStallCauseInstruction(child.ID, decision))
	b.WriteString("\n\nThis notice does not move, answer, or change either task. Review the child and decide the next action.")
	return b.String()
}

func childStallCauseInstruction(childID string, decision childstall.Decision) string {
	switch decision.Cause {
	case childstall.CauseInputRequired:
		if decision.QuestionID != "" {
			return fmt.Sprintf("The child is still waiting for your answer to its question %s. Answer it with message_task_kandev using task_id=%s and reply_to_question_id=%s.",
				decision.QuestionID, childID, decision.QuestionID)
		}
		return "The child is waiting for input in its own conversation (a question or permission request)."
	case childstall.CauseQuota:
		return "The child's agent stopped on a provider usage limit."
	case childstall.CauseExecutionError:
		return "The child's agent session failed."
	default:
		return "The child's turn ended without signalling step completion, so its workflow step did not advance."
	}
}

func isTaskNotFound(err error) bool {
	return errors.Is(err, repoerrors.ErrTaskNotFound)
}
