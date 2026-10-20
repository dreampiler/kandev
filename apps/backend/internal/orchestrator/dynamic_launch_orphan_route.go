package orchestrator

import (
	"context"
	"errors"

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
// recorded for entries still waiting in the session's queue. The chat
// handler's atomic queued send writes that row when it queues the entry, and
// a failed dispatch can record it before the entry is restored.
func (s *Service) queuedTranscriptMessageIDs(ctx context.Context, sessionID string) map[string]struct{} {
	if s.messageQueue == nil {
		return nil
	}
	status := s.messageQueue.GetStatus(ctx, sessionID)
	if status == nil || len(status.Entries) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(status.Entries))
	for index := range status.Entries {
		entry := &status.Entries[index]
		if recorded, _ := entry.Metadata[metaKeyUserMessageRecorded].(bool); !recorded {
			continue
		}
		if transcriptID, _ := entry.Metadata[messagequeue.MetadataDurableTranscriptMessageID].(string); transcriptID != "" {
			ids[transcriptID] = struct{}{}
		}
		ids[queuedTranscriptMessageID(entry, []string{entry.ID})] = struct{}{}
	}
	return ids
}
