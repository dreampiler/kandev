package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
)

// dynamicRelaunchHasResumeSource reports whether a manual relaunch can
// prompt the session through a cold resume. Without an executors_running row
// (an orphaned starting route swept after a restart) ensureSessionRunning has
// nothing to resume, so the relaunch must start a fresh execution instead.
// A lookup error other than not-found keeps the resume path, whose own
// failure stays visible.
func (s *Service) dynamicRelaunchHasResumeSource(ctx context.Context, sessionID string) bool {
	_, err := s.repo.GetExecutorRunningBySessionID(ctx, sessionID)
	return !errors.Is(err, models.ErrExecutorRunningNotFound)
}

// queuedTranscriptMessageIDs returns the transcript message IDs already
// recorded for entries the session's queue still owns. The snapshot includes
// retained rows an in-flight delivery reserved, and the dispatch claims cover
// ordinary rows the reservation moved out of the queue table. The chat
// handler's atomic queued send writes that row when it queues the entry, and a
// failed dispatch can record it before the entry is restored. A claim is scoped
// by session because queue state is deleted or transferred with the session.
// An unreadable queue state returns an error so callers abort instead of
// prompting with an incomplete held set.
func (s *Service) queuedTranscriptMessageIDs(ctx context.Context, sessionID string) (map[string]struct{}, error) {
	if s.messageQueue == nil {
		return nil, nil
	}
	ids := make(map[string]struct{})
	entries, _, err := s.messageQueue.SnapshotSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("snapshot session queue for held transcript ids: %w", err)
	}
	for index := range entries {
		addQueuedTranscriptMessageID(ids, &entries[index], []string{entries[index].ID})
	}
	if s.messageQueue.PendingQueueDispatchPersistenceAvailable() {
		reserved, err := s.messageQueue.ListPendingQueueDispatches(ctx)
		if err != nil {
			return nil, fmt.Errorf("list reserved queue dispatches for held transcript ids: %w", err)
		}
		for index := range reserved {
			claim := &reserved[index]
			if claim.Accepted || claim.Message.SessionID != sessionID {
				continue
			}
			addQueuedTranscriptMessageID(ids, &claim.Message, []string{claim.Message.ID})
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, nil
}

// addQueuedTranscriptMessageID records the row identities of queue-owned work.
// Durable claims can predate recording, so consumers match existing rows by ID.
func addQueuedTranscriptMessageID(ids map[string]struct{}, queuedMsg *messagequeue.QueuedMessage, sourceIDs []string) {
	if transcriptID, _ := queuedMsg.Metadata[messagequeue.MetadataDurableTranscriptMessageID].(string); transcriptID != "" {
		ids[transcriptID] = struct{}{}
	}
	ids[queuedTranscriptMessageID(queuedMsg, sourceIDs)] = struct{}{}
}
