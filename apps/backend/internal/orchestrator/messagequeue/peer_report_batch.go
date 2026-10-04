package messagequeue

import (
	"context"
	"fmt"
	"strings"
)

const (
	// MaxPeerReportBatchEntries bounds how many ordinary peer reports one
	// dispatch may deliver as a single turn.
	MaxPeerReportBatchEntries = 8
	// MaxPeerReportBatchBytes bounds the combined report content of one
	// batch, so a batch can never grow a prompt without limit.
	MaxPeerReportBatchBytes = 256 * 1024
)

// IsOrdinaryPeerReport reports whether an entry is an agent-authored peer
// report that may share a turn with the entries ahead of it. Everything else is
// an ordering boundary: user and operator instructions, managed input, durable
// delivery and lifecycle rows, plan comments, retained reservations, entries
// without a sender task, and entries whose attachments, entity references, or
// context files must stay separable.
func IsOrdinaryPeerReport(message *QueuedMessage) bool {
	if message == nil || message.QueuedBy != QueuedByAgent {
		return false
	}
	if message.IsDurableDelivery() || message.IsReservedInFlight() || len(message.Attachments) > 0 {
		return false
	}
	if PeerReportSenderTaskID(message) == "" {
		return false
	}
	return !hasBatchSeparableMetadata(message)
}

func hasBatchSeparableMetadata(message *QueuedMessage) bool {
	if message.Metadata == nil {
		return false
	}
	for _, key := range []string{MetadataEntityReferences, MetadataContextFiles} {
		if value, ok := message.Metadata[key]; ok && value != nil {
			return true
		}
	}
	return false
}

// ComposePeerReportBatch folds reports into the entry that leads them. The
// result keeps the first entry's identity, model, plan mode, and sender
// attribution, and renders every report separately with its own sender so no
// report loses its provenance.
func ComposePeerReportBatch(batch []*QueuedMessage) *QueuedMessage {
	if len(batch) == 0 {
		return nil
	}
	leader := *batch[0]
	if len(batch) == 1 {
		return &leader
	}
	var composed strings.Builder
	total := len(batch)
	for index, entry := range batch {
		if index > 0 {
			composed.WriteString("\n\n")
		}
		fmt.Fprintf(&composed, "[peer report %d/%d] from task %s\n%s",
			index+1, total, PeerReportSenderTaskID(entry), entry.Content)
	}
	leader.Content = composed.String()
	return &leader
}

// PeerReportSenderTaskID returns the attributed sender of a peer report.
func PeerReportSenderTaskID(message *QueuedMessage) string {
	if message == nil {
		return ""
	}
	return metadataString(message.Metadata, MetadataSenderTaskID)
}

// ReservePeerReportBatchWithAutoRunForSession reserves the FIFO head plus any
// consecutive ordinary peer reports behind it for a single dispatch. The head is
// reserved exactly as ReserveQueuedWithAutoRunForSession reserves it, so a
// non-batchable head keeps today's behavior, and autoRun keeps the same meaning:
// false only when Auto-run is OFF.
//
// A reserve that stops part-way returns the rows it already took alongside its
// error, because those rows have left the queue and only the caller can return
// them. Discarding them would lose their reports.
func (s *Service) ReservePeerReportBatchWithAutoRunForSession(
	ctx context.Context,
	identity QueueSessionIdentity,
) ([]*QueuedMessage, bool, error) {
	var batch []*QueuedMessage
	autoRun := true
	err := s.WithSessionAdmission(ctx, identity.SessionID, func(admittedCtx context.Context) error {
		var err error
		batch, autoRun, err = s.reservePeerReportBatch(admittedCtx, identity)
		return err
	})
	if err != nil {
		return batch, autoRun, err
	}
	return batch, autoRun, nil
}

func (s *Service) reservePeerReportBatch(
	ctx context.Context,
	identity QueueSessionIdentity,
) ([]*QueuedMessage, bool, error) {
	batch := make([]*QueuedMessage, 0, MaxPeerReportBatchEntries)
	batchBytes := 0
	for len(batch) < MaxPeerReportBatchEntries {
		message, autoRun, err := s.repo.ReserveHeadIfAutoRunForSession(ctx, identity)
		if err != nil {
			return batch, true, err
		}
		if !autoRun || message == nil {
			return batch, autoRun, nil
		}
		if len(batch) > 0 && message.ID == batch[len(batch)-1].ID {
			// A durable row keeps its queue position while its reservation is
			// in flight, so reserving again returns the same entry. The head is
			// already reserved; stop without releasing that reservation.
			return batch, true, nil
		}
		if len(batch) > 0 && !peerReportFitsBatch(batch[0], message, batchBytes) {
			// This entry cannot join the batch. Return it to its own position so
			// the next dispatch settles it in its own turn.
			if err := s.RequeueAtHeadForSession(ctx, identity, message); err != nil {
				// The entry left the queue and did not go back, so the caller has
				// to return it along with the rows this reserve already owns.
				// Appending keeps the walk order, which is the order the caller
				// replays the batch in.
				return append(batch, message), true, err
			}
			return batch, true, nil
		}
		batch = append(batch, message)
		if !IsOrdinaryPeerReport(message) {
			// The head is not an ordinary peer report, so it is delivered alone
			// exactly as the single-row path delivers it.
			return batch, true, nil
		}
		batchBytes += len(message.Content)
	}
	return batch, true, nil
}

// peerReportFitsBatch reports whether message can share one turn with leader:
// it must be an ordinary peer report that also agrees with the leading entry on
// the dispatch shape, and the batch must stay inside its byte ceiling.
func peerReportFitsBatch(leader, message *QueuedMessage, batchBytes int) bool {
	if !IsOrdinaryPeerReport(message) {
		return false
	}
	if message.Model != leader.Model || message.PlanMode != leader.PlanMode {
		return false
	}
	return batchBytes+len(message.Content) <= MaxPeerReportBatchBytes
}
