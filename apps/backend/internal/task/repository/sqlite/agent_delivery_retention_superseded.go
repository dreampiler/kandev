package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/task/models"
)

// A long-running tool (a shell command streaming output) can emit hundreds of
// in-progress tool_update events, each carrying the whole output so far. Every
// one is a durable inbox row, so a single tool call stores O(n^2) payload
// bytes until the payload grace expires. Once a later tool_update for the same
// tool call is projected, an earlier in-progress body is redundant transport
// evidence: projected bodies are read only by duplicate detection, which the
// compact hash receipt already serves. The superseded body is therefore
// cleared right away through the retention worker's locked proof and
// compaction statement; only the grace is replaced by the supersession proof.
// Unprojected rows, terminal rows and the newest update keep their bodies.
//
// The caller observed each row's tool call and progress-only status when it
// received the event, and holds the receive-time versioned hash. Equality with
// the persisted hash binds those observations to the stored row, so the
// writer transaction never reloads or decodes a body.
const (
	agentDeliverySupersededToolUpdateLimit = 16
	agentDeliverySupersededWorkTimeout     = time.Second
)

var errAgentDeliverySupersedingUnproved = errors.New("superseding tool update is not a projected stored receipt")

// CompactSupersededAgentDeliveryToolUpdates clears the raw payloads of the
// hinted earlier in-progress tool_update rows of superseding's stream once
// superseding, a later tool_update for the same tool call, is projected. The
// caller hints only rows it observed as progress of that tool call. Each row
// keeps its envelope, versioned hash, byte count and generic receipt, exactly
// as worker compaction leaves it. A row that fails any proof is left for the
// normal retention grace. It returns compacted rows and cleared bytes.
func (r *Repository) CompactSupersededAgentDeliveryToolUpdates(
	ctx context.Context,
	superseding *models.AgentDeliveryEvent,
	superseded []models.AgentDeliverySupersededToolUpdate,
) (int, int64, error) {
	if !supersedingAgentDeliveryToolUpdate(superseding) {
		return 0, 0, nil
	}
	hints := supersededAgentDeliveryHints(superseding.Sequence, superseded)
	if len(hints) == 0 {
		return 0, 0, nil
	}
	workCtx, cancel := context.WithTimeout(ctx, agentDeliverySupersededWorkTimeout)
	defer cancel()
	if err := r.prepareAgentDeliveryReloadStatement(workCtx); err != nil {
		return 0, 0, err
	}
	if err := r.prepareAgentDeliveryReceiptStatement(workCtx); err != nil {
		return 0, 0, err
	}
	now := r.nowUTC()
	tx, err := r.db.BeginTxx(workCtx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin superseded tool update compaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockAgentDeliveryRetentionWrites(workCtx, tx, r.db.DriverName(),
		[]string{superseding.SessionID}, []string{superseding.StreamID}, nil); err != nil {
		return 0, 0, fmt.Errorf("lock superseded tool update compaction: %w", err)
	}
	if err := r.checkSupersedingAgentDeliveryRowTx(workCtx, tx, superseding); err != nil {
		return 0, 0, err
	}
	selected := supersededAgentDeliverySelection(superseding.StreamID, hints)
	// The cutoff is now: supersession replaces the grace, every other proof of
	// the locked reload still applies. A zero byte budget loads no bodies.
	candidates, err := r.reloadAgentDeliverySelectedCandidates(workCtx, tx, selected, 0, now)
	if err != nil {
		return 0, 0, err
	}
	args, compacted, cleared := supersededAgentDeliveryCompactionArgs(candidates, superseding, hints)
	if compacted == 0 {
		return 0, 0, nil
	}
	if err := r.executeAgentDeliveryCompaction(workCtx, tx, append(args, []byte{}, now), compacted, true); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(workCtx, r.db.Rebind(`
		UPDATE agent_delivery_retention_state SET pruning_started_at = ?
		WHERE id = 1 AND pruning_started_at IS NULL`), now); err != nil {
		return 0, 0, fmt.Errorf("mark agent delivery pruning start: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit superseded tool update compaction: %w", err)
	}
	return compacted, cleared, nil
}

func supersedingAgentDeliveryToolUpdate(event *models.AgentDeliveryEvent) bool {
	return event != nil && event.StreamID != "" && event.Sequence > 1 && !event.Terminal &&
		event.EventType == streams.EventTypeToolUpdate && event.SessionID != "" &&
		event.IncarnationID != "" && event.HarnessGeneration > 0 && event.ContentHash != ""
}

// supersededAgentDeliveryHints keeps the newest valid hints older than the
// superseding sequence, keyed by sequence.
func supersededAgentDeliveryHints(
	supersedingSequence int64,
	superseded []models.AgentDeliverySupersededToolUpdate,
) map[int64]string {
	hints := make(map[int64]string, len(superseded))
	for index := len(superseded) - 1; index >= 0 && len(hints) < agentDeliverySupersededToolUpdateLimit; index-- {
		hint := superseded[index]
		if hint.Sequence <= 0 || hint.Sequence >= supersedingSequence || hint.ContentHash == "" {
			continue
		}
		if _, duplicate := hints[hint.Sequence]; !duplicate {
			hints[hint.Sequence] = hint.ContentHash
		}
	}
	return hints
}

func supersededAgentDeliverySelection(streamID string, hints map[int64]string) []agentDeliveryRetentionCandidate {
	selected := make([]agentDeliveryRetentionCandidate, 0, len(hints))
	for sequence := range hints {
		var candidate agentDeliveryRetentionCandidate
		candidate.event.StreamID = streamID
		candidate.event.Sequence = sequence
		candidate.compactionSource = true
		selected = append(selected, candidate)
	}
	return selected
}

// supersededAgentDeliveryCompactionArgs binds the proved candidates to the
// fixed-width payload compaction statement, preserving persisted byte counts.
func supersededAgentDeliveryCompactionArgs(
	candidates []agentDeliveryRetentionCandidate,
	superseding *models.AgentDeliveryEvent,
	hints map[int64]string,
) ([]any, int, int64) {
	args := make([]any, 0, agentDeliveryRetentionCandidateLimit*4+2)
	compacted := 0
	var cleared int64
	for index := range candidates {
		candidate := &candidates[index]
		if !supersededAgentDeliveryCandidate(candidate, superseding, hints[candidate.event.Sequence]) {
			continue
		}
		args = append(args, candidate.event.StreamID, candidate.event.Sequence,
			candidate.event.ContentHash, candidate.payloadLength)
		compacted++
		cleared += candidate.payloadLength
	}
	for slot := compacted; slot < agentDeliveryRetentionCandidateLimit; slot++ {
		args = append(args, nil, nil, nil, nil)
	}
	return args, compacted, cleared
}

// checkSupersedingAgentDeliveryRowTx proves, under the stream lock, that the
// retention format is supported and that the caller's event is the stored,
// projected receipt for its stream position with the same versioned hash.
func (r *Repository) checkSupersedingAgentDeliveryRowTx(
	ctx context.Context,
	tx *sqlx.Tx,
	superseding *models.AgentDeliveryEvent,
) error {
	var formatVersion int
	if err := tx.QueryRowxContext(ctx, `
		SELECT format_version FROM agent_delivery_retention_state WHERE id = 1`).Scan(&formatVersion); err != nil {
		return fmt.Errorf("read agent delivery retention format marker: %w", err)
	}
	if formatVersion != agentDeliveryRetentionFormatVersion {
		return fmt.Errorf("unsupported agent delivery retention format %d", formatVersion)
	}
	stored, projectedAt, found, err := r.findAgentDeliveryEventTx(ctx, tx, superseding.StreamID, superseding.Sequence)
	if err != nil {
		return err
	}
	if !found || !projectedAt.Valid || stored.Terminal || stored.EventType != streams.EventTypeToolUpdate ||
		stored.SessionID != superseding.SessionID || stored.IncarnationID != superseding.IncarnationID ||
		stored.HarnessGeneration != superseding.HarnessGeneration ||
		stored.HashVersion != agentDeliveryPayloadHashVersion || stored.ContentHash != superseding.ContentHash {
		return errAgentDeliverySupersedingUnproved
	}
	return nil
}

// supersededAgentDeliveryCandidate applies the worker's locked can_prune proof
// and adds the supersession proof: an earlier nonterminal tool_update in the
// same stream identity whose persisted versioned hash and byte count match the
// row the caller observed as progress of the same tool call.
func supersededAgentDeliveryCandidate(
	candidate *agentDeliveryRetentionCandidate,
	superseding *models.AgentDeliveryEvent,
	hintHash string,
) bool {
	event := &candidate.event
	return hintHash != "" && candidate.cursorIdentityValid && candidate.canPrune &&
		!candidate.payloadPrunedAt.Valid && candidate.payloadLength > 0 &&
		event.PayloadBytes == candidate.payloadLength &&
		event.HashVersion == agentDeliveryPayloadHashVersion && event.ContentHash == hintHash &&
		event.StreamID == superseding.StreamID && event.Sequence < superseding.Sequence &&
		event.SessionID == superseding.SessionID && event.IncarnationID == superseding.IncarnationID &&
		event.HarnessGeneration == superseding.HarnessGeneration &&
		!event.Terminal && event.EventType == streams.EventTypeToolUpdate
}
