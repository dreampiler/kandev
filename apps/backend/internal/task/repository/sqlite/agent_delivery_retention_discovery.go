package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	agentDeliveryRetentionPhaseCompaction = "compaction"
	agentDeliveryRetentionPhaseReceipts   = "receipts"
)

// agentDeliveryReplayWindow is the replay window a retirement checkpoint must
// already have left: rows projected at or before ageCutoff, or at least events
// sequences behind the projected cursor.
type agentDeliveryReplayWindow struct {
	ageCutoff time.Time
	events    int
}

func (r *Repository) selectAgentDeliveryEventRetentionPages(ctx context.Context, tx *sqlx.Tx, progress agentDeliveryRetentionProgress, limit int, cutoff time.Time, replay agentDeliveryReplayWindow) ([]agentDeliveryRetentionCandidate, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}
	compactionFirst := progress.eventSweepPhase != agentDeliveryRetentionPhaseReceipts
	firstLimit := (limit + 1) / 2
	if compactionFirst {
		firstLimit = limit
	}
	var compact, receipts []agentDeliveryRetentionCandidate
	var wrapped bool
	var err error
	if compactionFirst {
		compact, err = r.selectAgentDeliveryCompactionCandidates(ctx, tx, progress, firstLimit, cutoff)
		if err == nil {
			receipts, wrapped, err = r.selectAgentDeliveryReceiptCandidates(ctx, tx, progress, limit-len(compact), replay)
		}
	} else {
		receipts, wrapped, err = r.selectAgentDeliveryReceiptCandidates(ctx, tx, progress, firstLimit, replay)
		if err == nil {
			compact, err = r.selectAgentDeliveryCompactionCandidates(ctx, tx, progress, limit-len(receipts), cutoff)
		}
	}
	if err != nil {
		return nil, false, err
	}
	// Both discovery indexes contain disjoint rows; the combined proof page is bounded.
	compact = append(compact, receipts...)
	combined := compact
	sort.Slice(combined, func(i, j int) bool {
		return combined[i].event.StreamID < combined[j].event.StreamID || (combined[i].event.StreamID == combined[j].event.StreamID && combined[i].event.Sequence < combined[j].event.Sequence)
	})
	return combined, wrapped, nil
}

// Unknown event types can never compact or retire; the locked proof rejects
// them by type. Skipping them in discovery keeps that permanent residue from
// consuming every bounded page on each frontier wrap. The list is the proof's
// own known-type classifier, including projected session-state events.
var agentDeliveryCompactableEventTypeFilter = `i.event_type IN (` + agentDeliveryRetentionKnownTypesSQL + `)`

func agentDeliveryCompactionCandidateQuery(progress agentDeliveryRetentionProgress, limit int, cutoff time.Time) (string, []any) {
	filters := []string{"i.payload_pruned_at IS NULL", "i.projected_at IS NOT NULL", "i.projected_at <= ?", agentDeliveryCompactableEventTypeFilter}
	args := []any{cutoff}
	if progress.lastCompactionAt.Valid {
		filters = append(filters, "(i.projected_at,i.stream_id,i.sequence) > (?,?,?)")
		args = append(args, progress.lastCompactionAt.Time, progress.lastCompactionStreamID, progress.lastCompactionSequence)
	}
	return agentDeliveryDiscoveryQuery(filters, args, limit, "i.projected_at,i.stream_id,i.sequence")
}

func (r *Repository) selectAgentDeliveryCompactionCandidates(ctx context.Context, tx *sqlx.Tx, progress agentDeliveryRetentionProgress, limit int, cutoff time.Time) ([]agentDeliveryRetentionCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	query, args := agentDeliveryCompactionCandidateQuery(progress, limit, cutoff)
	candidates, err := r.scanAgentDeliveryDiscoveryCandidates(ctx, tx, query, args)
	if err == nil && len(candidates) == 0 && progress.lastCompactionAt.Valid {
		query, args = agentDeliveryCompactionCandidateQuery(agentDeliveryRetentionProgress{}, limit, cutoff)
		candidates, err = r.scanAgentDeliveryDiscoveryCandidates(ctx, tx, query, args)
	}
	for index := range candidates {
		candidates[index].compactionSource = true
	}
	return candidates, err
}

// Receipt discovery feeds prefix retirement, which can only remove the
// contiguous run that starts right after a stream's retired_sequence. It
// therefore visits retirement heads instead of every compacted row: a stream
// qualifies only when its head row is compacted and already outside the replay
// window, and each visit pages forward from that head. Compacted rows inside
// the replay window are not rescanned, so the frontier no longer spends whole
// passes walking receipts that cannot retire yet.
//
// The persisted (last_stream_id, last_sequence) frontier keeps its meaning as
// the last visited receipt. A stream stays eligible while its checkpoint
// reached that receipt (its page retired completely); a stream that stopped at
// a protected row waits for the next wrap.
func agentDeliveryRetirementHeadQuery(lastStreamID string, lastSequence int64, limit int, replay agentDeliveryReplayWindow) (string, []any) {
	// The cursor table drives the scan in stream order; the head probe is a
	// correlated primary-key seek so no plan can walk the compacted inbox.
	query := `SELECT c.stream_id, c.retired_sequence, c.projected_sequence
	 FROM agent_delivery_cursors c
	 WHERE c.projected_sequence > c.retired_sequence`
	var args []any
	if lastStreamID != "" {
		query += ` AND (c.stream_id > ? OR (c.stream_id = ? AND c.retired_sequence >= ?))`
		args = append(args, lastStreamID, lastStreamID, lastSequence)
	}
	query += ` AND EXISTS (
	  SELECT 1 FROM agent_delivery_inbox head
	  WHERE head.stream_id = c.stream_id AND head.sequence = c.retired_sequence + 1
	   AND head.payload_pruned_at IS NOT NULL
	   AND (head.sequence <= c.projected_sequence - ? OR head.projected_at <= ?
	        OR ` + agentDeliveryCompletedStreamSQL("c") + `))
	 ORDER BY c.stream_id LIMIT ?`
	return query, append(args, int64(replay.events), replay.ageCutoff, limit)
}

// agentDeliveryRetirementPageQuery reads compacted rows forward from one
// retirement head through a primary-key range bounded by the projected cursor.
func agentDeliveryRetirementPageQuery(streamID string, retiredSequence, projectedSequence int64, limit int) (string, []any) {
	query := `SELECT i.stream_id,i.sequence,i.session_id,i.projected_at
	 FROM agent_delivery_inbox i
	 WHERE i.stream_id = ? AND i.sequence > ? AND i.sequence <= ? AND i.payload_pruned_at IS NOT NULL
	 ORDER BY i.sequence LIMIT ?`
	return query, []any{streamID, retiredSequence, projectedSequence, limit}
}

type agentDeliveryRetirementHead struct {
	streamID          string
	retiredSequence   int64
	projectedSequence int64
}

func (r *Repository) selectAgentDeliveryReceiptCandidates(ctx context.Context, tx *sqlx.Tx, progress agentDeliveryRetentionProgress, limit int, replay agentDeliveryReplayWindow) ([]agentDeliveryRetentionCandidate, bool, error) {
	if limit <= 0 {
		return nil, false, nil
	}
	candidates, err := r.selectAgentDeliveryRetirementPages(ctx, tx, progress.lastStreamID, progress.lastSequence, limit, replay)
	if err != nil || len(candidates) > 0 || progress.lastStreamID == "" {
		return candidates, false, err
	}
	candidates, err = r.selectAgentDeliveryRetirementPages(ctx, tx, "", 0, limit, replay)
	return candidates, true, err
}

func (r *Repository) selectAgentDeliveryRetirementPages(ctx context.Context, tx *sqlx.Tx, lastStreamID string, lastSequence int64, limit int, replay agentDeliveryReplayWindow) ([]agentDeliveryRetentionCandidate, error) {
	heads, err := r.scanAgentDeliveryRetirementHeads(ctx, tx, lastStreamID, lastSequence, limit, replay)
	if err != nil {
		return nil, err
	}
	var candidates []agentDeliveryRetentionCandidate
	for _, head := range heads {
		remaining := limit - len(candidates)
		if remaining <= 0 {
			break
		}
		query, args := agentDeliveryRetirementPageQuery(head.streamID, head.retiredSequence, head.projectedSequence, remaining)
		page, err := r.scanAgentDeliveryDiscoveryCandidates(ctx, tx, query, args)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, contiguousAgentDeliveryRetirementPage(page, head.retiredSequence)...)
	}
	return candidates, nil
}

func (r *Repository) scanAgentDeliveryRetirementHeads(ctx context.Context, tx *sqlx.Tx, lastStreamID string, lastSequence int64, limit int, replay agentDeliveryReplayWindow) ([]agentDeliveryRetirementHead, error) {
	query, args := agentDeliveryRetirementHeadQuery(lastStreamID, lastSequence, limit, replay)
	rows, err := tx.QueryxContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("scan agent delivery retirement heads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var heads []agentDeliveryRetirementHead
	for rows.Next() {
		var head agentDeliveryRetirementHead
		if err := rows.Scan(&head.streamID, &head.retiredSequence, &head.projectedSequence); err != nil {
			return nil, fmt.Errorf("scan agent delivery retirement head: %w", err)
		}
		heads = append(heads, head)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent delivery retirement heads: %w", err)
	}
	return heads, nil
}

// contiguousAgentDeliveryRetirementPage keeps the gap-free run after the
// checkpoint. A raw row interrupts it; compaction discovery owns that row.
func contiguousAgentDeliveryRetirementPage(page []agentDeliveryRetentionCandidate, retiredSequence int64) []agentDeliveryRetentionCandidate {
	next := retiredSequence + 1
	for index, candidate := range page {
		if candidate.event.Sequence != next {
			return page[:index]
		}
		next++
	}
	return page
}

func agentDeliveryDiscoveryQuery(filters []string, args []any, limit int, order string) (string, []any) {
	query := `SELECT i.stream_id,i.sequence,i.session_id,i.projected_at
	 FROM agent_delivery_inbox i WHERE ` + strings.Join(filters, " AND ") + ` ORDER BY ` + order + ` LIMIT ?`
	return query, append(args, limit)
}

func (r *Repository) scanAgentDeliveryDiscoveryCandidates(ctx context.Context, tx *sqlx.Tx, query string, args []any) ([]agentDeliveryRetentionCandidate, error) {
	rows, err := tx.QueryxContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("scan agent delivery discovery identities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var candidates []agentDeliveryRetentionCandidate
	for rows.Next() {
		var candidate agentDeliveryRetentionCandidate
		if err := rows.Scan(&candidate.event.StreamID, &candidate.event.Sequence, &candidate.event.SessionID, &candidate.projectedAt); err != nil {
			return nil, fmt.Errorf("scan agent delivery discovery identity: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent delivery discovery identities: %w", err)
	}
	return candidates, nil
}

func (r *Repository) reloadAgentDeliverySelectedCandidates(ctx context.Context, tx *sqlx.Tx, selected []agentDeliveryRetentionCandidate, byteLimit int64, cutoff time.Time) ([]agentDeliveryRetentionCandidate, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	if len(selected) > agentDeliveryRetentionCandidateLimit {
		return nil, fmt.Errorf("agent delivery locked reload exceeds candidate limit")
	}
	sources := make(map[string]map[int64]bool)
	for _, candidate := range selected {
		if sources[candidate.event.StreamID] == nil {
			sources[candidate.event.StreamID] = make(map[int64]bool)
		}
		sources[candidate.event.StreamID][candidate.event.Sequence] = candidate.compactionSource
	}
	_, args := agentDeliverySelectedCandidateQuery(selected, byteLimit, cutoff)
	prepared, err := r.agentDeliveryReloadStatement()
	if err != nil {
		return nil, err
	}
	statement := tx.StmtxContext(ctx, prepared)
	defer func() { _ = statement.Close() }()
	rows, err := statement.QueryxContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("scan agent delivery retention candidates: %w", err)
	}
	candidates, err := scanAgentDeliveryRetentionCandidates(rows)
	for index := range candidates {
		candidates[index].compactionSource = sources[candidates[index].event.StreamID][candidates[index].event.Sequence]
	}
	return candidates, err
}

var agentDeliveryLockedReloadQuery = func() string {
	values := strings.TrimSuffix(strings.Repeat("(?,?),", agentDeliveryRetentionCandidateLimit), ",")
	query, _ := agentDeliveryRetentionFilteredQuery([]string{"(i.stream_id,i.sequence) IN (" + values + ")"}, nil, 0, 0, time.Time{}, true)
	return query
}()

func agentDeliverySelectedCandidateQuery(selected []agentDeliveryRetentionCandidate, byteLimit int64, cutoff time.Time) (string, []any) {
	args := make([]any, 0, agentDeliveryRetentionCandidateLimit*2+9)
	for _, candidate := range selected {
		args = append(args, candidate.event.StreamID, candidate.event.Sequence)
	}
	for slot := len(selected); slot < agentDeliveryRetentionCandidateLimit; slot++ {
		args = append(args, nil, nil)
	}
	args = append(args, len(selected), cutoff, byteLimit, true, byteLimit, byteLimit, true, byteLimit, byteLimit)
	return agentDeliveryLockedReloadQuery, args
}

func advanceAgentDeliveryEventRetentionProgress(progress *agentDeliveryRetentionProgress, candidates []agentDeliveryRetentionCandidate, receiptWrapped bool) {
	var latestCompaction *agentDeliveryRetentionCandidate
	receiptSeen := false
	for index := range candidates {
		candidate := &candidates[index]
		if !candidate.compactionSource {
			progress.lastStreamID = candidate.event.StreamID
			progress.lastSequence = candidate.event.Sequence
			receiptSeen = true
			continue
		}
		if latestCompaction == nil || compactionCandidateAfter(candidate, latestCompaction) {
			latestCompaction = candidate
		}
	}
	if latestCompaction != nil {
		progress.lastCompactionAt = sql.NullTime{Time: latestCompaction.projectedAt.Time, Valid: true}
		progress.lastCompactionStreamID = latestCompaction.event.StreamID
		progress.lastCompactionSequence = latestCompaction.event.Sequence
	}
	if !receiptSeen && receiptWrapped {
		progress.lastStreamID = ""
		progress.lastSequence = 0
	}
	if progress.eventSweepPhase == agentDeliveryRetentionPhaseReceipts {
		progress.eventSweepPhase = agentDeliveryRetentionPhaseCompaction
	} else {
		progress.eventSweepPhase = agentDeliveryRetentionPhaseReceipts
	}
}

func compactionCandidateAfter(candidate, previous *agentDeliveryRetentionCandidate) bool {
	if !candidate.projectedAt.Time.Equal(previous.projectedAt.Time) {
		return candidate.projectedAt.Time.After(previous.projectedAt.Time)
	}
	return candidate.event.StreamID > previous.event.StreamID || (candidate.event.StreamID == previous.event.StreamID && candidate.event.Sequence > previous.event.Sequence)
}

// Only increasing persisted frontiers justify continuing a protected-only phase.
// Wrapping or changing sweep preference alone cannot keep a pass repeating.
func agentDeliveryDiscoveryAdvanced(previous agentDeliveryRetentionProgress, candidates []agentDeliveryRetentionCandidate, prompts []agentDeliveryPromptRetentionCandidate, receiptWrapped bool) bool {
	next := previous
	advanceAgentDeliveryEventRetentionProgress(&next, candidates, receiptWrapped)
	if next.lastStreamID > previous.lastStreamID || (next.lastStreamID == previous.lastStreamID && next.lastSequence > previous.lastSequence) {
		return true
	}
	if len(prompts) > 0 && prompts[len(prompts)-1].submissionID > previous.lastSubmissionID {
		return true
	}
	if !next.lastCompactionAt.Valid {
		return false
	}
	if !previous.lastCompactionAt.Valid {
		return true
	}
	return next.lastCompactionAt.Time.After(previous.lastCompactionAt.Time) ||
		(next.lastCompactionAt.Time.Equal(previous.lastCompactionAt.Time) &&
			(next.lastCompactionStreamID > previous.lastCompactionStreamID ||
				(next.lastCompactionStreamID == previous.lastCompactionStreamID && next.lastCompactionSequence > previous.lastCompactionSequence)))
}
