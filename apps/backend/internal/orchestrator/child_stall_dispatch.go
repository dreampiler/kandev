package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/childstall"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// SetChildStallProducer installs the producer whose candidates the queue
// drain rechecks before dispatching a child stall alert.
func (s *Service) SetChildStallProducer(producer *ChildStallProducer) {
	s.childStallProducer = producer
}

// pruneStaleChildStallAlerts removes pending child stall alerts that no longer
// need delivery: alerts whose every candidate stopped qualifying, and alerts
// whose every candidate is superseded by a newer pending alert for the same
// child task. Alerts with at least one still-needed candidate stay queued. Only
// child stall alert entries are considered, so user, agent, and other server
// entries are never touched, and entries are only removed — never reordered.
func (s *Service) pruneStaleChildStallAlerts(ctx context.Context, identity messagequeue.QueueSessionIdentity) {
	producer := s.childStallProducer
	if producer == nil || s.messageQueue == nil {
		return
	}
	entries, _, err := s.messageQueue.SnapshotSessionForIdentity(ctx, identity)
	if err != nil {
		return
	}
	newest := producer.newestEligibleAlertKeysByChild(ctx, entries)
	for i := range entries {
		entry := &entries[i]
		if !messagequeue.IsChildStallAlert(entry) || entry.IsReservedInFlight() || entry.IsDeliveryAttempted() || entry.IsDurableDelivery() {
			continue
		}
		keep, reason := producer.alertEntryDisposition(ctx, entry, newest)
		if keep {
			continue
		}
		removed, removeErr := producer.removePendingNotice(ctx, s.messageQueue, identity, entry.ID)
		switch {
		case removeErr != nil && !errors.Is(removeErr, messagequeue.ErrEntryNotFound):
			s.logger.Warn("failed to remove stale child stall alert",
				zap.String("session_id", identity.SessionID), zap.String("queue_id", entry.ID), zap.Error(removeErr))
		case removed != nil:
			recordChildStallOutcome(reason)
			s.publishQueueStatusEventForIdentity(ctx, identity)
		}
	}
}

// alertOrderKey orders pending alert entries by enqueue time, with the FIFO
// position as a tie-break, so one alert can be identified as newer than another.
type alertOrderKey struct {
	queuedAt time.Time
	position int64
}

func (k alertOrderKey) after(other alertOrderKey) bool {
	if k.queuedAt.Equal(other.queuedAt) {
		return k.position > other.position
	}
	return k.queuedAt.After(other.queuedAt)
}

func alertOrderKeyOf(entry *messagequeue.QueuedMessage) alertOrderKey {
	return alertOrderKey{queuedAt: entry.QueuedAt, position: entry.Position}
}

// newestEligibleAlertKeysByChild maps each child task to the order key of the
// newest pending alert whose candidate for that child still qualifies. Only a
// still-eligible newer alert can supersede an older one, so an eligible alert is
// never dropped in favor of a stale sibling.
func (p *ChildStallProducer) newestEligibleAlertKeysByChild(
	ctx context.Context,
	entries []messagequeue.QueuedMessage,
) map[string]alertOrderKey {
	newest := make(map[string]alertOrderKey)
	for i := range entries {
		entry := &entries[i]
		if !messagequeue.IsChildStallAlert(entry) {
			continue
		}
		key := alertOrderKeyOf(entry)
		for _, descriptor := range messagequeue.ChildStallAlertDescriptors(entry) {
			if descriptor.ChildTaskID == "" || !p.alertDescriptorEligible(ctx, descriptor.TurnID) {
				continue
			}
			if current, ok := newest[descriptor.ChildTaskID]; !ok || key.after(current) {
				newest[descriptor.ChildTaskID] = key
			}
		}
	}
	return newest
}

// alertEntryDisposition reports whether a pending alert entry must stay queued,
// and why it is removed when it must not. An entry is kept when at least one of
// its candidates is still eligible and not superseded by a newer eligible alert
// for the same child. A candidate with no child or turn identity is treated as
// eligible and never superseded, so unclear provenance is preserved.
func (p *ChildStallProducer) alertEntryDisposition(
	ctx context.Context,
	entry *messagequeue.QueuedMessage,
	newest map[string]alertOrderKey,
) (keep bool, reason string) {
	descriptors := messagequeue.ChildStallAlertDescriptors(entry)
	if len(descriptors) == 0 {
		return true, ""
	}
	key := alertOrderKeyOf(entry)
	eligible := false
	for _, descriptor := range descriptors {
		if !p.alertDescriptorEligible(ctx, descriptor.TurnID) {
			continue
		}
		eligible = true
		if !newest[descriptor.ChildTaskID].after(key) {
			return true, ""
		}
	}
	if !eligible {
		return false, "stale_at_dispatch"
	}
	return false, "superseded_at_dispatch"
}

// alertDescriptorEligible reports whether one folded candidate still qualifies.
// A candidate with no turn identity, or an unreadable one, counts as eligible so
// a read failure never drops an alert.
func (p *ChildStallProducer) alertDescriptorEligible(ctx context.Context, turnID string) bool {
	return turnID == "" || p.sourceStillStalled(ctx, turnID)
}

func (p *ChildStallProducer) sourceStillStalled(ctx context.Context, turnID string) bool {
	turn, err := p.store.GetTurn(ctx, turnID)
	if err != nil || turn == nil {
		return err != nil
	}
	start, ok := models.LoadChildStallStart(turn.Metadata)
	if !ok {
		return false
	}
	evidence, err := p.gatherEvidence(ctx, turn, start, p.now())
	if err != nil {
		return true
	}
	switch childstall.Classify(evidence, p.policy).Outcome {
	case childstall.OutcomeQualified, childstall.OutcomeHeld, childstall.OutcomeSettling:
		return true
	default:
		return false
	}
}

// queuedChildStallNudge checks a server reminder before any prompt is recorded.
func (p *ChildStallProducer) queuedChildStallNudge(ctx context.Context, entry *messagequeue.QueuedMessage) (bool, error) {
	turnID, _ := entry.Metadata["turn_id"].(string)
	if turnID == "" {
		return true, nil
	}
	turn, err := p.store.GetTurn(ctx, turnID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if turn == nil || turn.TaskID != entry.TaskID {
		return false, nil
	}
	start, ok := models.LoadChildStallStart(turn.Metadata)
	if !ok {
		return false, nil
	}
	evidence, err := p.gatherEvidence(ctx, turn, start, p.now())
	if err != nil {
		return false, err
	}
	decision := childstall.Classify(evidence, p.policy)
	return decision.Outcome == childstall.OutcomeQualified && decision.Cause == childstall.CauseMissingCompletionSignal, nil
}

func (s *Service) checkQueuedChildStallNotice(ctx context.Context, entry *messagequeue.QueuedMessage, reservation *queuedDispatchReservation) (dispatch, deferDrain bool) {
	if s.childStallProducer == nil || entry.IsDeliveryAttempted() || entry.IsDurableDelivery() {
		return true, false
	}
	var current bool
	var err error
	switch {
	case messagequeue.IsChildStallNudge(entry):
		current, err = s.childStallProducer.queuedChildStallNudge(ctx, entry)
	case messagequeue.IsChildStallAlert(entry):
		current, err = s.childStallProducer.prepareQueuedChildStallAlert(ctx, entry)
	default:
		return true, false
	}
	if err == nil && current {
		return true, false
	}
	if err == nil {
		err = s.messageQueue.AcknowledgeQueued(context.WithoutCancel(ctx), entry)
		if err == nil {
			s.publishQueueStatusEvent(ctx, entry.SessionID)
			return false, false
		}
	}
	s.logger.Warn("deferring child stall notice", zap.String("queue_id", entry.ID), zap.Error(err))
	s.requeueMessageForReservation(ctx, reservation, entry, entry.QueuedBy)
	return false, true
}

func (p *ChildStallProducer) prepareQueuedChildStallAlert(ctx context.Context, entry *messagequeue.QueuedMessage) (bool, error) {
	descriptors := messagequeue.ChildStallAlertDescriptors(entry)
	if len(descriptors) == 0 {
		return true, nil
	}
	parent, err := p.store.GetTask(ctx, entry.TaskID)
	if isTaskNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if parent == nil || parent.ArchivedAt != nil || models.IsTerminalTaskState(parent.State) {
		return false, nil
	}
	kept := make([]messagequeue.ChildStallAlertDescriptor, 0, len(descriptors))
	changed := false
	for _, descriptor := range descriptors {
		current, eligible, err := p.currentChildStallAlert(ctx, parent, descriptor)
		if err != nil {
			return false, err
		}
		if eligible {
			kept = append(kept, current)
		}
		changed = changed || !eligible || current != descriptor
	}
	if len(kept) == 0 {
		return false, nil
	}
	if changed {
		parts := make([]string, 0, len(kept))
		for _, item := range kept {
			parts = append(parts, formatChildStallPrompt(&models.Task{ID: item.ChildTaskID, Title: item.ChildTaskTitle}, &models.Turn{ID: item.TurnID},
				item.StepName, childstall.Decision{Cause: childstall.Cause(item.Cause), QuestionID: item.QuestionID}))
		}
		entry.Content = strings.Join(parts, "\n\n")
		entry.Metadata = messagequeue.WithChildStallAlertDescriptors(entry.Metadata, kept)
	}
	return true, nil
}

func (p *ChildStallProducer) currentChildStallAlert(ctx context.Context, parent *models.Task, item messagequeue.ChildStallAlertDescriptor) (messagequeue.ChildStallAlertDescriptor, bool, error) {
	if item.TurnID == "" {
		return item, true, nil
	}
	turn, err := p.store.GetTurn(ctx, item.TurnID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, err
	}
	if turn == nil || turn.TaskID != item.ChildTaskID {
		return item, false, nil
	}
	start, ok := models.LoadChildStallStart(turn.Metadata)
	if !ok || start.ParentTaskID != parent.ID || start.WorkspaceID != parent.WorkspaceID {
		return item, false, nil
	}
	decision, err := p.classifyCurrentChildStall(ctx, turn, start)
	if errors.Is(err, models.ErrTaskSessionNotFound) {
		return item, false, nil
	}
	if err != nil {
		return item, false, err
	}
	item, eligible := refreshQueuedChildStallAlert(item, decision)
	return item, eligible, nil
}

func refreshQueuedChildStallAlert(item messagequeue.ChildStallAlertDescriptor, decision childstall.Decision) (messagequeue.ChildStallAlertDescriptor, bool) {
	switch decision.Outcome {
	case childstall.OutcomeQualified:
		cause := string(decision.Cause)
		if decision.QuestionID != "" || cause != item.Cause {
			item.QuestionID = decision.QuestionID
		}
		item.Cause = cause
		return item, true
	case childstall.OutcomeHeld, childstall.OutcomeSettling:
		return item, true
	default:
		return item, false
	}
}

// ErrChildStallUnavailable means the child-turn stalled producer is not running.
var ErrChildStallUnavailable = errors.New("child stall producer is not available")

// RetryChildStall makes a failed child stall alert eligible again under its
// original identity. The caller must be authorized for the child task/session.
func (s *Service) RetryChildStall(ctx context.Context, taskID, sessionID, turnID string) (bool, error) {
	if err := s.authorizeSessionControl(ctx, sessionID); err != nil {
		return false, err
	}
	if err := s.authorizeTaskSessionPair(ctx, taskID, sessionID); err != nil {
		return false, err
	}
	producer := s.childStallProducer
	if producer == nil {
		return false, ErrChildStallUnavailable
	}
	return producer.RetryFailed(ctx, sessionID, turnID)
}
