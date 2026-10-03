package orchestrator

import (
	"context"
	"errors"

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

// pruneStaleChildStallAlerts removes pending child stall alerts whose every
// source candidate stopped qualifying, so a stale alert never creates a model
// turn. Alerts with at least one eligible source stay queued.
func (s *Service) pruneStaleChildStallAlerts(ctx context.Context, identity messagequeue.QueueSessionIdentity) {
	producer := s.childStallProducer
	if producer == nil || s.messageQueue == nil {
		return
	}
	entries, _, err := s.messageQueue.SnapshotSessionForIdentity(ctx, identity)
	if err != nil {
		return
	}
	for i := range entries {
		entry := &entries[i]
		if !messagequeue.IsChildStallAlert(entry) || producer.anyAlertEligible(ctx, entry) {
			continue
		}
		if _, removed, takeErr := s.messageQueue.TakeQueuedEntryForSession(ctx, identity, entry.ID); takeErr != nil {
			s.logger.Warn("failed to remove stale child stall alert",
				zap.String("session_id", identity.SessionID), zap.String("queue_id", entry.ID), zap.Error(takeErr))
		} else if removed {
			recordChildStallOutcome("stale_at_dispatch")
			s.publishQueueStatusEventForIdentity(ctx, identity)
		}
	}
}

// anyAlertEligible reports whether any candidate folded into entry still
// qualifies. Unreadable candidates count as eligible so a read failure never
// drops an alert.
func (p *ChildStallProducer) anyAlertEligible(ctx context.Context, entry *messagequeue.QueuedMessage) bool {
	alerts, _ := entry.Metadata[messagequeue.MetadataChildStallAlerts].([]interface{})
	if len(alerts) == 0 {
		return true
	}
	for _, raw := range alerts {
		descriptor, _ := raw.(map[string]interface{})
		turnID, _ := descriptor["turn_id"].(string)
		if turnID == "" || p.sourceStillStalled(ctx, turnID) {
			return true
		}
	}
	return false
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

// ErrChildStallUnavailable means the child-turn stalled producer is not running.
var ErrChildStallUnavailable = errors.New("child stall producer is not available")

// RetryChildStall makes a failed child stall alert eligible again under its
// original identity. The caller must be authorized for the child task/session.
func (s *Service) RetryChildStall(ctx context.Context, taskID, sessionID, turnID string) (bool, error) {
	if err := s.authorizeTaskSessionPair(ctx, taskID, sessionID); err != nil {
		return false, err
	}
	producer := s.childStallProducer
	if producer == nil {
		return false, ErrChildStallUnavailable
	}
	return producer.RetryFailed(ctx, sessionID, turnID)
}
