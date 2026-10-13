package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/childstall"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

const parentQuestionStatusPendingValue = "pending"

// gatherEvidence reads the structured state a classification needs. Any read
// failure is returned so the candidate stays retryable.
func (p *ChildStallProducer) gatherEvidence(
	ctx context.Context,
	turn *models.Turn,
	start models.ChildStallStart,
	now time.Time,
) (childstall.Evidence, error) {
	evidence := childstall.Evidence{
		Start:      start,
		Settlement: models.ChildStallSettlement(turn.Metadata),
	}
	if turn.CompletedAt != nil {
		evidence.SettledFor = now.Sub(*turn.CompletedAt)
	}
	child, err := p.store.GetTask(ctx, turn.TaskID)
	if errors.Is(err, repoerrors.ErrTaskNotFound) {
		return evidence, nil
	}
	if err != nil {
		return evidence, err
	}
	latestTransition, err := p.store.GetLatestTaskStepTransitionID(ctx, child.ID)
	if err != nil {
		return evidence, err
	}
	evidence.Child = &childstall.ChildSnapshot{
		ParentID:           child.ParentID,
		WorkspaceID:        child.WorkspaceID,
		State:              child.State,
		Archived:           child.ArchivedAt != nil,
		LatestTransitionID: latestTransition,
	}
	if err := p.gatherSessionEvidence(ctx, turn.TaskSessionID, child, &evidence); err != nil {
		return evidence, err
	}
	return evidence, nil
}

func (p *ChildStallProducer) gatherSessionEvidence(
	ctx context.Context,
	sessionID string,
	child *models.Task,
	evidence *childstall.Evidence,
) error {
	session, err := p.store.GetTaskSession(ctx, sessionID)
	if err != nil {
		return err
	}
	evidence.SessionState = session.State
	evidence.SessionErrorClass = session.RouteErrorClass
	active, err := p.hasActiveTurn(ctx, sessionID)
	if err != nil {
		return err
	}
	evidence.SessionActiveTurn = active
	pending, err := p.store.ListPendingInteractions(ctx, models.PendingInteractionFilter{SessionIDs: []string{sessionID}})
	if err != nil {
		return err
	}
	evidence.PendingInputs = pendingInputsFromMessages(pending)
	if p.svc != nil {
		evidence.StepRequiresSignal = p.svc.WorkflowStepRequiresCompletionSignal(ctx, child.WorkflowStepID)
	}
	if err := p.gatherCompletionSignalState(ctx, session, child, evidence); err != nil {
		return err
	}
	return nil
}

// gatherCompletionSignalState reads whether the child task already holds an
// accepted completion signal for its current step, and whether a completion
// signal for that step was refused by the ownership/profile gate. Both are read
// across the task's sessions, not only the settled turn's session: a session
// substitution moves the accepted signal and the refusal onto a new session
// while the settled turn keeps naming the old one, which is how a reminder
// repeats after the agent already signalled.
func (p *ChildStallProducer) gatherCompletionSignalState(
	ctx context.Context,
	turnSession *models.TaskSession,
	child *models.Task,
	evidence *childstall.Evidence,
) error {
	sessions, err := p.store.ListTaskSessions(ctx, child.ID)
	if err != nil {
		return err
	}
	var newestRejection time.Time
	seen := make(map[string]struct{}, len(sessions)+1)
	for _, session := range sessions {
		if session == nil || session.ID == "" {
			continue
		}
		seen[session.ID] = struct{}{}
		observeCompletionSignalState(session, child.WorkflowStepID, evidence, &newestRejection)
	}
	// The settled turn's session owns the read even when the list is stale or
	// does not include it, so add it explicitly.
	if turnSession != nil && turnSession.ID != "" {
		if _, ok := seen[turnSession.ID]; !ok {
			observeCompletionSignalState(turnSession, child.WorkflowStepID, evidence, &newestRejection)
		}
	}
	return nil
}

// observeCompletionSignalState folds one session's stored completion state for
// the current step into the evidence. A signal for the step marks the task as
// signalled; the newest recorded rejection for the step provides the block
// reason shown in the reminder.
func observeCompletionSignalState(
	session *models.TaskSession,
	stepID string,
	evidence *childstall.Evidence,
	newestRejection *time.Time,
) {
	if signal, has := models.LoadPendingStepSignal(session.Metadata); has && signal.StepID == stepID {
		evidence.PendingStepSignal = true
	}
	if rejection, has := models.LoadStepCompleteRejection(session.Metadata); has && rejection.StepID == stepID {
		if rejection.At.After(*newestRejection) {
			*newestRejection = rejection.At
			evidence.BlockedReason = rejection.Reason
		}
	}
}

func (p *ChildStallProducer) hasActiveTurn(ctx context.Context, sessionID string) (bool, error) {
	turn, err := p.store.GetActiveTurnBySessionID(ctx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return turn != nil, nil
}

func pendingInputsFromMessages(messages []*models.Message) []childstall.PendingInput {
	inputs := make([]childstall.PendingInput, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		inputs = append(inputs, childstall.PendingInput{
			MessageID:        message.ID,
			ParentQuestionID: pendingParentQuestionID(message),
		})
	}
	return inputs
}

func pendingParentQuestionID(message *models.Message) string {
	if message.Type != models.MessageTypeClarificationRequest || message.Metadata == nil {
		return ""
	}
	isParentQuestion, _ := message.Metadata[models.MetaKeyParentQuestion].(bool)
	status, _ := message.Metadata[models.MetaKeyParentQuestionStatus].(string)
	if !isParentQuestion || status != parentQuestionStatusPendingValue {
		return ""
	}
	questionID, _ := message.Metadata[models.MetaKeyParentQuestionID].(string)
	if questionID == "" {
		questionID = message.ID
	}
	return questionID
}

// promoteHeld decides whether a candidate held on a pending parent question
// becomes an alert. A parent that cannot receive the alert is reported as
// qualified so delivery records the wait and notifies the operator.
func (p *ChildStallProducer) promoteHeld(ctx context.Context, held childstall.Decision, now time.Time) childstall.Decision {
	question, err := p.store.GetMessage(ctx, held.QuestionID)
	if err != nil || question == nil {
		return held
	}
	status, _ := question.Metadata[models.MetaKeyParentQuestionStatus].(string)
	parentID, _ := question.Metadata[models.MetaKeyParentQuestionParentID].(string)
	promotion := childstall.QuestionPromotion{QuestionPending: status == parentQuestionStatusPendingValue}
	if !promotion.QuestionPending || parentID == "" {
		return withQuestion(childstall.PromoteHeld(promotion), held.QuestionID)
	}
	parentSession, unavailable, err := p.resolveParentSession(ctx, parentID)
	if err != nil {
		return held
	}
	if unavailable {
		return withQuestion(childstall.Decision{Outcome: childstall.OutcomeQualified, Cause: childstall.CauseInputRequired}, held.QuestionID)
	}
	if promotion.QuestionQueued, err = p.questionQueuedForParent(ctx, parentSession, held.QuestionID); err != nil {
		return held
	}
	if promotion.ParentIdle, err = p.parentIdle(ctx, parentSession); err != nil {
		return held
	}
	promotion.ParentSettledAfterQuestion, err = p.parentSettledAfter(ctx, parentSession, question.CreatedAt, now)
	if err != nil {
		return held
	}
	return withQuestion(childstall.PromoteHeld(promotion), held.QuestionID)
}

func withQuestion(decision childstall.Decision, questionID string) childstall.Decision {
	if decision.Outcome == childstall.OutcomeQualified || decision.Outcome == childstall.OutcomeHeld {
		decision.QuestionID = questionID
	}
	return decision
}

func (p *ChildStallProducer) questionQueuedForParent(
	ctx context.Context,
	parentSession *models.TaskSession,
	questionID string,
) (bool, error) {
	queue := p.messageQueue()
	if queue == nil {
		return false, errChildStallQueueUnavailable
	}
	identity, err := queue.ResolveSessionIdentity(ctx, parentSession.TaskID, parentSession.ID)
	if err != nil {
		return false, err
	}
	entries, _, err := queue.SnapshotSessionForIdentity(ctx, identity)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if queuedID, _ := entry.Metadata[models.MetaKeyParentQuestionID].(string); queuedID == questionID {
			return true, nil
		}
	}
	return false, nil
}

func (p *ChildStallProducer) parentIdle(ctx context.Context, parentSession *models.TaskSession) (bool, error) {
	if parentSession.State == models.TaskSessionStateRunning || parentSession.State == models.TaskSessionStateStarting {
		return false, nil
	}
	active, err := p.hasActiveTurn(ctx, parentSession.ID)
	return !active, err
}

func (p *ChildStallProducer) parentSettledAfter(
	ctx context.Context,
	parentSession *models.TaskSession,
	questionCreatedAt time.Time,
	now time.Time,
) (bool, error) {
	latest, err := p.store.GetLatestCompletedTurnBySessionID(ctx, parentSession.ID)
	if err != nil {
		return false, err
	}
	if latest == nil || latest.CompletedAt == nil {
		// A primary session started after the question replaced the one that
		// received it, so the question never reached it.
		return parentSession.StartedAt.After(questionCreatedAt), nil
	}
	return latest.StartedAt.After(questionCreatedAt) && now.Sub(*latest.CompletedAt) >= p.policy.SettleGrace, nil
}

// resolveParentSession returns the parent's current primary session and
// whether it cannot receive a prompt (missing, failed, cancelled, completed).
func (p *ChildStallProducer) resolveParentSession(ctx context.Context, parentTaskID string) (*models.TaskSession, bool, error) {
	session, err := p.store.GetPrimarySessionByTaskID(ctx, parentTaskID)
	if errors.Is(err, repoerrors.ErrNoPrimarySession) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if session == nil {
		return nil, true, nil
	}
	switch session.State {
	case models.TaskSessionStateFailed, models.TaskSessionStateCancelled, models.TaskSessionStateCompleted:
		return session, true, nil
	}
	return session, false, nil
}

func (p *ChildStallProducer) messageQueue() *messagequeue.Service {
	if p.svc == nil {
		return nil
	}
	return p.svc.messageQueue
}
