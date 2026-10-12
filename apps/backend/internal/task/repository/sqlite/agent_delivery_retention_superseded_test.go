package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

func TestCompactSupersededAgentDeliveryToolUpdates(t *testing.T) {
	for _, engine := range []string{"SQLite", "Postgres"} {
		t.Run(engine, func(t *testing.T) {
			repo := newRetentionTestRepo(t, engine)
			ctx := context.Background()
			const (
				sessionID = "superseded-tool-session"
				streamID  = "superseded-tool-stream"
			)
			incarnationID := "incarnation-" + sessionID
			seedAgentDeliveryTestSession(t, repo, sessionID, incarnationID)
			now := time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC)
			repo.clockNow = func() time.Time { return now }

			wires := []streams.AgentEvent{
				{Type: streams.EventTypeToolCall, ToolCallID: "exec-x", ToolStatus: "in_progress"},
				{Type: streams.EventTypeToolUpdate, ToolCallID: "exec-x", ToolStatus: "in_progress", Text: strings.Repeat("a", 4096)},
				{Type: streams.EventTypeToolUpdate, ToolCallID: "exec-x", ToolStatus: "in_progress", Text: strings.Repeat("a", 8192)},
				{Type: streams.EventTypeToolUpdate, ToolCallID: "exec-y", ToolStatus: "in_progress", Text: "other tool"},
				{Type: streams.EventTypeToolUpdate, ToolCallID: "exec-x", ToolStatus: "in_progress", Text: strings.Repeat("a", 12288)},
				{Type: streams.EventTypeToolUpdate, ToolCallID: "exec-x", ToolStatus: "complete", Text: strings.Repeat("a", 16384)},
				{Type: streams.EventTypeToolUpdate, ToolCallID: "exec-x", ToolStatus: "in_progress", Text: "late progress"},
				{Type: streams.EventTypeToolUpdate, ToolCallID: "exec-x", ToolStatus: "in_progress", Text: "later progress"},
			}
			events := make([]*models.AgentDeliveryEvent, 0, len(wires))
			for index, wire := range wires {
				payload, err := json.Marshal(wire)
				if err != nil {
					t.Fatalf("marshal event %d: %v", index+1, err)
				}
				event := &models.AgentDeliveryEvent{
					SessionID: sessionID, IncarnationID: incarnationID, HarnessGeneration: 1,
					StreamID: streamID, Sequence: int64(index + 1), EventType: wire.Type,
					Payload: payload, ReceivedAt: now,
				}
				if inserted, err := repo.ReceiveAgentDeliveryEvent(ctx, event, int64(len(wires))); err != nil || !inserted {
					t.Fatalf("receive event %d = %v, err %v", index+1, inserted, err)
				}
				events = append(events, event)
			}
			// Sequence 7 stays unprojected; 8 is projected out of order, so the
			// contiguous projected cursor stops at 6.
			for _, event := range append(append([]*models.AgentDeliveryEvent{}, events[:6]...), events[7]) {
				if _, err := repo.ProjectAgentDeliveryEvent(ctx, event, completedRetentionEventEffect(event)); err != nil {
					t.Fatalf("project event %d: %v", event.Sequence, err)
				}
			}

			hint := func(event *models.AgentDeliveryEvent) models.AgentDeliverySupersededToolUpdate {
				return models.AgentDeliverySupersededToolUpdate{Sequence: event.Sequence, ContentHash: event.ContentHash}
			}
			// The newer in-progress update supersedes the earlier one.
			rows, cleared, err := repo.CompactSupersededAgentDeliveryToolUpdates(ctx, events[2],
				[]models.AgentDeliverySupersededToolUpdate{hint(events[1])})
			if err != nil || rows != 1 || cleared != int64(len(events[1].Payload)) {
				t.Fatalf("compact superseded by seq 3 = %d rows, %d bytes, err %v", rows, cleared, err)
			}
			assertSupersededToolUpdateCompacted(t, repo, events[1])

			// An already compacted row, the superseding row itself and later
			// sequences are not compacted again.
			rows, cleared, err = repo.CompactSupersededAgentDeliveryToolUpdates(ctx, events[5],
				[]models.AgentDeliverySupersededToolUpdate{hint(events[1]), hint(events[2]), hint(events[4]), hint(events[5]),
					{Sequence: 9, ContentHash: events[7].ContentHash}})
			if err != nil || rows != 2 || cleared != int64(len(events[2].Payload)+len(events[4].Payload)) {
				t.Fatalf("compact superseded by complete = %d rows, %d bytes, err %v", rows, cleared, err)
			}
			for _, event := range []*models.AgentDeliveryEvent{events[2], events[4]} {
				assertSupersededToolUpdateCompacted(t, repo, event)
			}

			// An unprojected row keeps its body even when a later update for the
			// same tool is projected, and a hint whose receive-time hash does not
			// match the persisted row proves nothing.
			rows, _, err = repo.CompactSupersededAgentDeliveryToolUpdates(ctx, events[7],
				[]models.AgentDeliverySupersededToolUpdate{hint(events[6]), {Sequence: 4, ContentHash: events[2].ContentHash}})
			if err != nil || rows != 0 {
				t.Fatalf("compact unprojected and mismatched rows = %d, err %v; want none", rows, err)
			}
			for _, event := range []*models.AgentDeliveryEvent{events[0], events[3], events[5], events[6], events[7]} {
				assertSupersededToolUpdateRetained(t, repo, event)
			}

			// An unprojected superseding update proves nothing.
			unprojected := *events[6]
			if _, _, err := repo.CompactSupersededAgentDeliveryToolUpdates(ctx, &unprojected,
				[]models.AgentDeliverySupersededToolUpdate{hint(events[4])}); !errors.Is(err, errAgentDeliverySupersedingUnproved) {
				t.Fatalf("unprojected superseding error = %v, want unproved", err)
			}
			// Changed content under a projected receipt proves nothing either.
			changed := *events[7]
			changed.ContentHash = events[3].ContentHash
			if _, _, err := repo.CompactSupersededAgentDeliveryToolUpdates(ctx, &changed,
				[]models.AgentDeliverySupersededToolUpdate{hint(events[3])}); !errors.Is(err, errAgentDeliverySupersedingUnproved) {
				t.Fatalf("changed superseding error = %v, want unproved", err)
			}
			assertSupersededToolUpdateRetained(t, repo, events[3])

			// Compact receipts still classify replayed duplicates and conflicts.
			replay := *events[1]
			replay.ContentHash = ""
			if inserted, err := repo.ReceiveAgentDeliveryEvent(ctx, &replay, int64(len(wires))); err != nil || inserted {
				t.Fatalf("replay superseded duplicate = %v, err %v; want identical duplicate", inserted, err)
			}
			conflict := *events[2]
			conflict.ContentHash = ""
			conflict.Payload = []byte(`{"type":"tool_update","tool_call_id":"exec-x","tool_status":"in_progress","text":"changed"}`)
			if _, err := repo.ReceiveAgentDeliveryEvent(ctx, &conflict, int64(len(wires))); !errors.Is(err, repoerrors.ErrAgentDeliveryEventConflict) {
				t.Fatalf("changed superseded replay error = %v, want conflict", err)
			}

			// Compaction leaves the cursors and generic receipts alone and marks
			// the database as pruned like worker compaction does.
			cursor, err := repo.GetAgentDeliveryCursor(ctx, streamID)
			if err != nil {
				t.Fatalf("read cursor: %v", err)
			}
			if cursor.ReceivedSequence != 8 || cursor.ProjectedSequence != 6 || cursor.RetiredSequence != 0 {
				t.Fatalf("cursor after superseded compaction = %+v", cursor)
			}
			assertRetentionRowCount(t, repo, "agent_delivery_effects", "stream_id = ?", streamID, 7)
			var pruningStarted sql.NullTime
			if err := repo.db.QueryRowxContext(ctx, `SELECT pruning_started_at FROM agent_delivery_retention_state WHERE id = 1`).Scan(&pruningStarted); err != nil {
				t.Fatalf("read pruning marker: %v", err)
			}
			if !pruningStarted.Valid {
				t.Fatal("superseded compaction did not record pruning start")
			}
		})
	}
}

func TestCompactSupersededAgentDeliveryToolUpdatesKeepsRecoveryEvidence(t *testing.T) {
	repo := newRetentionTestRepo(t, "SQLite")
	ctx := context.Background()
	const (
		sessionID = "superseded-tool-blocked-session"
		streamID  = "superseded-tool-blocked-stream"
	)
	incarnationID := "incarnation-" + sessionID
	seedAgentDeliveryTestSession(t, repo, sessionID, incarnationID)
	events := make([]*models.AgentDeliveryEvent, 0, 3)
	for index, status := range []string{"in_progress", "in_progress", "in_progress"} {
		payload, err := json.Marshal(streams.AgentEvent{
			Type: streams.EventTypeToolUpdate, ToolCallID: "exec-z", ToolStatus: status,
			Text: strings.Repeat("z", 1024*(index+1)),
		})
		if err != nil {
			t.Fatal(err)
		}
		event := &models.AgentDeliveryEvent{
			SessionID: sessionID, IncarnationID: incarnationID, HarnessGeneration: 1,
			StreamID: streamID, Sequence: int64(index + 1), EventType: streams.EventTypeToolUpdate,
			Payload: payload, ReceivedAt: time.Now().UTC(),
		}
		if _, err := repo.ReceiveAgentDeliveryEvent(ctx, event, 3); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	// Sequence 1 has no completed generic effect; the proof keeps its body.
	if _, err := repo.ProjectAgentDeliveryEvent(ctx, events[0], nil); err != nil {
		t.Fatal(err)
	}
	for _, event := range events[1:] {
		if _, err := repo.ProjectAgentDeliveryEvent(ctx, event, completedRetentionEventEffect(event)); err != nil {
			t.Fatal(err)
		}
	}
	if rows, _, err := repo.CompactSupersededAgentDeliveryToolUpdates(ctx, events[1],
		[]models.AgentDeliverySupersededToolUpdate{{Sequence: 1, ContentHash: events[0].ContentHash}}); err != nil || rows != 0 {
		t.Fatalf("compact row without generic receipt = %d, err %v; want none", rows, err)
	}
	assertSupersededToolUpdateRetained(t, repo, events[0])

	// An open recovery block for the incarnation preserves event evidence.
	if _, err := repo.db.Exec(repo.db.Rebind(`
		INSERT INTO session_recovery_blocks (id, session_id, incarnation_id, expected_generation,
			reason, consumer_reference, state, created_at, updated_at)
		VALUES (?, ?, ?, 1, 'delivery_uncertain', 'agent_delivery', 'open', ?, ?)`),
		"superseded-tool-block", sessionID, incarnationID, time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatalf("open recovery block: %v", err)
	}
	if rows, _, err := repo.CompactSupersededAgentDeliveryToolUpdates(ctx, events[2],
		[]models.AgentDeliverySupersededToolUpdate{{Sequence: 2, ContentHash: events[1].ContentHash}}); err != nil || rows != 0 {
		t.Fatalf("compact under open recovery block = %d, err %v; want none", rows, err)
	}
	assertSupersededToolUpdateRetained(t, repo, events[1])
}

func assertSupersededToolUpdateCompacted(t *testing.T, repo *Repository, event *models.AgentDeliveryEvent) {
	t.Helper()
	var payload []byte
	var contentHash string
	var hashVersion int
	var prunedAt sql.NullTime
	var payloadBytes int64
	if err := repo.db.QueryRowxContext(context.Background(), repo.db.Rebind(`
		SELECT payload, content_hash, hash_version, payload_pruned_at, payload_bytes
		FROM agent_delivery_inbox WHERE stream_id = ? AND sequence = ?`), event.StreamID, event.Sequence).Scan(
		&payload, &contentHash, &hashVersion, &prunedAt, &payloadBytes,
	); err != nil {
		t.Fatalf("read compacted event %d: %v", event.Sequence, err)
	}
	if len(payload) != 0 || !prunedAt.Valid || hashVersion != agentDeliveryPayloadHashVersion ||
		contentHash != agentDeliveryEventHash(event) || payloadBytes != int64(len(event.Payload)) {
		t.Fatalf("event %d compact receipt = payload %d bytes, hash %q v%d, pruned %v, bytes %d",
			event.Sequence, len(payload), contentHash, hashVersion, prunedAt, payloadBytes)
	}
}

func assertSupersededToolUpdateRetained(t *testing.T, repo *Repository, event *models.AgentDeliveryEvent) {
	t.Helper()
	var payload []byte
	var prunedAt sql.NullTime
	if err := repo.db.QueryRowxContext(context.Background(), repo.db.Rebind(`
		SELECT payload, payload_pruned_at FROM agent_delivery_inbox WHERE stream_id = ? AND sequence = ?`),
		event.StreamID, event.Sequence).Scan(&payload, &prunedAt); err != nil {
		t.Fatalf("read retained event %d: %v", event.Sequence, err)
	}
	if prunedAt.Valid || string(payload) != string(event.Payload) {
		t.Fatalf("event %d body was cleared (pruned %v, %d bytes)", event.Sequence, prunedAt, len(payload))
	}
}
