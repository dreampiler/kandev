package lifecycle

import (
	"context"
	"fmt"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agentctl/journal"
	"github.com/kandev/kandev/internal/task/models"
)

// Include only terminal SQL records needed to explain retained unknown journal
// entries. Completed history outside that inventory is not recovery authority.
func nativeResumeSQLRows(ctx context.Context, execution *AgentExecution, status *agentctl.DeliveryStatus, peer []journal.Submission, repository AgentDeliveryRepository) ([]*models.AgentDeliverySubmission, error) {
	rows, err := activeBackendSubmissions(ctx, repository, execution.SessionID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		seen[row.ID] = true
	}
	for _, submission := range peer {
		if seen[submission.ID] || submission.State != journal.SubmissionInterruptedUnknown {
			continue
		}
		row, err := nativeResumeSubmissionMetadata(ctx, repository, submission.ID)
		if err != nil {
			return nil, err
		}
		if !peerMatchesTerminalHistory(execution, status, submission, row) {
			return nil, fmt.Errorf("%w: native resume terminal evidence disagrees: submission_id=%q %s", ErrDurableAdoptionBlocked, submission.ID, nativeTerminalMismatchDetail(execution, status, submission, row))
		}
		cursor, err := repository.GetAgentDeliveryCursor(ctx, submission.StreamID)
		if err != nil {
			return nil, err
		}
		if !nativeTerminalCursorMatches(cursor, submission, row.TerminalSequence) {
			return nil, fmt.Errorf("%w: native resume terminal is not projected", ErrDurableAdoptionBlocked)
		}
		seen[row.ID] = true
		rows = append(rows, row)
	}
	return rows, nil
}

func peerMatchesTerminalHistory(execution *AgentExecution, status *agentctl.DeliveryStatus, peer journal.Submission, row *models.AgentDeliverySubmission) bool {
	if row == nil || !nativeResumeTerminalState(row.State) || row.Outcome != string(row.State) {
		return false
	}
	return peer.State == journal.SubmissionInterruptedUnknown && peer.TerminalEventRetained && peer.TerminalSequence > 0 &&
		nativeTerminalOwnerMatches(execution, status, peer, row) && row.TerminalStreamID == peer.StreamID &&
		row.TerminalSequence > 0 && uint64(row.TerminalSequence) == peer.TerminalSequence && peer.Hash != "" && row.PayloadHash == peer.Hash
}

func nativeTerminalOwnerMatches(execution *AgentExecution, status *agentctl.DeliveryStatus, peer journal.Submission, row *models.AgentDeliverySubmission) bool {
	return peer.SessionID == execution.SessionID && row.SessionID == peer.SessionID && peer.ID == row.ID &&
		peer.IncarnationID == status.IncarnationID && row.IncarnationID == peer.IncarnationID &&
		peer.HarnessGeneration > 0 && peer.HarnessGeneration <= status.HarnessGeneration && row.HarnessGeneration == int64(peer.HarnessGeneration) &&
		peer.StreamID != "" && (peer.HarnessGeneration < status.HarnessGeneration || peer.StreamID == status.StreamID)
}

func nativeResumeTerminalState(state models.DeliverySubmissionState) bool {
	return state == models.DeliverySubmissionCompleted || state == models.DeliverySubmissionFailed || state == models.DeliverySubmissionCancelled
}

func (m *Manager) hasProjectedTerminalRetirement(ctx context.Context, execution *AgentExecution) (bool, error) {
	client, release := execution.AcquireAgentCtlClient()
	defer release()
	if client == nil || m.streamManager == nil || m.streamManager.deliveryRepository() == nil {
		return false, ErrDurableAdoptionEvidenceUnavailable
	}
	status, err := m.nativeResumeDeliveryStatus(ctx, execution, client)
	if err != nil || status == nil {
		return false, err
	}
	peer, err := client.ListDeliverySubmissions(ctx, execution.SessionID)
	if err != nil {
		return false, err
	}
	rows, err := nativeResumeSQLRows(ctx, execution, status, peer, m.streamManager.deliveryRepository())
	if err != nil {
		return false, err
	}
	if err := validateNativeResumeSQLRows(execution, status, peer, rows, false); err != nil {
		return false, err
	}
	ids, err := nativeResumeRetirementIDs(execution, status, peer, rows)
	if err != nil {
		return false, err
	}
	terminals := make(map[string]bool, len(rows))
	for _, row := range rows {
		terminals[row.ID] = nativeResumeTerminalState(row.State)
	}
	for _, id := range ids {
		if !terminals[id] {
			return false, nil
		}
	}
	return len(ids) > 0, nil
}

func nativeResumeSubmissionMetadata(ctx context.Context, repository AgentDeliveryRepository, id string) (*models.AgentDeliverySubmission, error) {
	if reader, ok := repository.(agentDeliverySubmissionMetadataReader); ok {
		return reader.GetAgentDeliverySubmissionMetadata(ctx, id)
	}
	if reader, ok := repository.(agentDeliverySubmissionReader); ok {
		return reader.GetAgentDeliverySubmission(ctx, id)
	}
	return nil, ErrDurableAdoptionEvidenceUnavailable
}

func nativeTerminalCursorMatches(cursor *models.AgentDeliveryCursor, peer journal.Submission, sequence int64) bool {
	return cursor != nil && cursor.StreamID == peer.StreamID && cursor.SessionID == peer.SessionID &&
		cursor.IncarnationID == peer.IncarnationID && cursor.HarnessGeneration == int64(peer.HarnessGeneration) &&
		cursor.ReceivedSequence == cursor.ProjectedSequence && cursor.RetiredSequence >= 0 && cursor.RetiredSequence <= cursor.ProjectedSequence && cursor.ProjectedSequence >= sequence
}

// deliveryMismatchDetail contains only the first mismatched owner or terminal field.
func deliveryMismatchDetail(reason, field string, expected, observed any) string {
	return fmt.Sprintf("reason=%s field=%s expected=%v observed=%v", reason, field, expected, observed)
}

func terminalStateDetail(value string) string {
	switch value {
	case "":
		return "empty_state"
	case "completed", "failed", "cancelled", "interrupted_unknown", "accepted", "running":
		return value
	default:
		return "other"
	}
}

func nativeTerminalMismatchDetail(execution *AgentExecution, status *agentctl.DeliveryStatus, peer journal.Submission, row *models.AgentDeliverySubmission) string {
	const recordMismatch = "terminal_record_mismatch"
	switch {
	case row == nil:
		return deliveryMismatchDetail("terminal_row_missing", "sql_row_present", true, false)
	case !nativeResumeTerminalState(row.State):
		return deliveryMismatchDetail(recordMismatch, "sql_state", "terminal", terminalStateDetail(string(row.State)))
	case row.Outcome != string(row.State):
		return deliveryMismatchDetail(recordMismatch, "sql_outcome", row.State, terminalStateDetail(row.Outcome))
	case peer.State != journal.SubmissionInterruptedUnknown:
		return deliveryMismatchDetail(recordMismatch, "peer_state", journal.SubmissionInterruptedUnknown, terminalStateDetail(string(peer.State)))
	case !peer.TerminalEventRetained:
		return deliveryMismatchDetail("terminal_event_not_retained", "peer_terminal_retained", true, false)
	case peer.TerminalSequence == 0:
		return deliveryMismatchDetail("terminal_event_not_retained", "peer_terminal_sequence", "positive", peer.TerminalSequence)
	}
	if !nativeTerminalOwnerMatches(execution, status, peer, row) {
		return nativeTerminalOwnerMismatchDetail(execution, status, peer, row)
	}
	if row.TerminalStreamID != peer.StreamID {
		return deliveryMismatchDetail("terminal_projection_mismatch", "sql_terminal_stream", peer.StreamID, row.TerminalStreamID)
	}
	if row.TerminalSequence <= 0 || uint64(row.TerminalSequence) != peer.TerminalSequence {
		return deliveryMismatchDetail("terminal_projection_mismatch", "sql_terminal_sequence", peer.TerminalSequence, row.TerminalSequence)
	}
	// The existing guard already checked payload identity. Never render its values.
	return "reason=terminal_payload_identity_mismatch field=payload_identity"
}

func nativeTerminalOwnerMismatchDetail(execution *AgentExecution, status *agentctl.DeliveryStatus, peer journal.Submission, row *models.AgentDeliverySubmission) string {
	const reason = "terminal_owner_mismatch"
	switch {
	case peer.SessionID != execution.SessionID:
		return deliveryMismatchDetail(reason, "peer_session_id", execution.SessionID, peer.SessionID)
	case row.SessionID != peer.SessionID:
		return deliveryMismatchDetail(reason, "sql_session_id", peer.SessionID, row.SessionID)
	case peer.ID != row.ID:
		return deliveryMismatchDetail(reason, "sql_submission_id", peer.ID, row.ID)
	case peer.IncarnationID != status.IncarnationID:
		return deliveryMismatchDetail(reason, "peer_incarnation_id", status.IncarnationID, peer.IncarnationID)
	case row.IncarnationID != peer.IncarnationID:
		return deliveryMismatchDetail(reason, "sql_incarnation_id", peer.IncarnationID, row.IncarnationID)
	case peer.HarnessGeneration == 0 || peer.HarnessGeneration > status.HarnessGeneration:
		return deliveryMismatchDetail(reason, "peer_harness_generation", fmt.Sprintf("1..%d", status.HarnessGeneration), peer.HarnessGeneration)
	case row.HarnessGeneration != int64(peer.HarnessGeneration):
		return deliveryMismatchDetail(reason, "sql_harness_generation", peer.HarnessGeneration, row.HarnessGeneration)
	case peer.StreamID == "":
		return deliveryMismatchDetail(reason, "peer_stream_id", true, false)
	default:
		return deliveryMismatchDetail(reason, "peer_stream_id", status.StreamID, peer.StreamID)
	}
}
