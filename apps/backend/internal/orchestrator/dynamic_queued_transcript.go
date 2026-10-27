package orchestrator

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
)

// queuedTranscriptMessageIDs returns recorded transcript rows whose work the
// queue still owns, including retained reservations and ordinary dispatch claims.
// An unreadable inventory cannot attest that queued work is absent.
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

func addQueuedTranscriptMessageID(ids map[string]struct{}, queuedMsg *messagequeue.QueuedMessage, sourceIDs []string) {
	if recorded, _ := queuedMsg.Metadata[metaKeyUserMessageRecorded].(bool); !recorded {
		return
	}
	if transcriptID, _ := queuedMsg.Metadata[messagequeue.MetadataDurableTranscriptMessageID].(string); transcriptID != "" {
		ids[transcriptID] = struct{}{}
	}
	ids[queuedTranscriptMessageID(queuedMsg, sourceIDs)] = struct{}{}
}
