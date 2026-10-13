package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

// Every real stream starts with agent_capabilities and interleaves session
// state events with output. Projected state rows must compact and retire with
// the output around them, or no stream checkpoint can ever leave zero.
var retentionRealStreamTypes = []string{
	streams.EventTypeAgentCapabilities,
	streams.EventTypeSessionStatus,
	streams.EventTypeSessionModels,
	streams.EventTypeSessionMode,
	streams.EventTypeAvailableCommands,
	streams.EventTypeMCPAttachment,
	streams.EventTypeTurnStarted,
	streams.EventTypeMessageChunk,
	streams.EventTypeToolCall,
	streams.EventTypeMCPAttachment,
	streams.EventTypeToolUpdate,
	streams.EventTypeContextWindow,
	streams.EventTypePermissionRequest,
	streams.EventTypePermissionCancelled,
	streams.EventTypeReasoning,
	streams.EventTypeUsageObservation,
	streams.EventTypeRateLimit,
	streams.EventTypeSessionInfo,
	streams.EventTypeForegroundIdle,
	streams.EventTypeMessageChunk,
}

type retentionStateFixture struct {
	repo    *Repository
	baseNow time.Time
	now     time.Time
}

func newRetentionStateFixture(t *testing.T, engine string) *retentionStateFixture {
	t.Helper()
	fixture := &retentionStateFixture{repo: newRetentionTestRepo(t, engine)}
	fixture.baseNow = time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	fixture.now = fixture.baseNow
	fixture.repo.clockNow = func() time.Time { return fixture.now }
	return fixture
}

// session creates a task session in the requested lifecycle position.
func (f *retentionStateFixture) session(t *testing.T, taskID, sessionID string, state models.TaskSessionState, primary bool, startedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.repo.GetTask(ctx, taskID); err != nil {
		if err := f.repo.CreateTask(ctx, &models.Task{ID: taskID, WorkspaceID: retentionTestWorkspaceID(t, f.repo), Title: taskID}); err != nil {
			t.Fatalf("create task %s: %v", taskID, err)
		}
	}
	if err := f.repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: sessionID, TaskID: taskID, QueueIncarnationID: "incarnation-" + sessionID, State: state,
	}); err != nil {
		t.Fatalf("create session %s: %v", sessionID, err)
	}
	if _, err := f.repo.db.ExecContext(ctx, f.repo.db.Rebind(`
		UPDATE task_sessions SET state = ?, is_primary = ?, started_at = ? WHERE id = ?`),
		string(state), boolToInt(primary), startedAt, sessionID); err != nil {
		t.Fatalf("position session %s: %v", sessionID, err)
	}
}

// stream receives and projects one event per type, each with its completed
// generic receipt, exactly as the lifecycle consumer commits them.
func (f *retentionStateFixture) stream(t *testing.T, sessionID, streamID string, types []string) []*models.AgentDeliveryEvent {
	t.Helper()
	ctx := context.Background()
	events := make([]*models.AgentDeliveryEvent, 0, len(types))
	for index, eventType := range types {
		wire := streams.AgentEvent{Type: eventType, Text: "retention state fixture " + strconv.Itoa(index+1)}
		if eventType != streams.EventTypeMessageChunk && eventType != streams.EventTypeReasoning {
			wire.Data = map[string]any{"fixture": eventType, "index": index + 1}
		}
		payload, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("marshal %s: %v", eventType, err)
		}
		event := &models.AgentDeliveryEvent{
			SessionID: sessionID, IncarnationID: "incarnation-" + sessionID, HarnessGeneration: 1,
			StreamID: streamID, Sequence: int64(index + 1), EventType: eventType,
			Payload: payload, ReceivedAt: f.now,
		}
		if inserted, err := f.repo.ReceiveAgentDeliveryEvent(ctx, event, int64(index+1)); err != nil || !inserted {
			t.Fatalf("receive %s/%d = %v, %v", streamID, index+1, inserted, err)
		}
		effect := retentionGenericEffect(event, f.now)
		if eventType == streams.EventTypeMessageChunk || eventType == streams.EventTypeReasoning {
			if _, err := f.repo.ProjectCanonicalAgentDeliveryEvent(ctx, event, effect); err != nil {
				t.Fatalf("project canonical %s/%d: %v", streamID, index+1, err)
			}
		} else if _, err := f.repo.ProjectAgentDeliveryEvent(ctx, event, effect); err != nil {
			t.Fatalf("project %s/%d: %v", streamID, index+1, err)
		}
		events = append(events, event)
	}
	return events
}

func (f *retentionStateFixture) ageProjection(t *testing.T, streamID string, projectedAt time.Time) {
	t.Helper()
	if _, err := f.repo.db.ExecContext(context.Background(), f.repo.db.Rebind(`
		UPDATE agent_delivery_inbox SET projected_at = ? WHERE stream_id = ?`), projectedAt, streamID); err != nil {
		t.Fatalf("age stream %s: %v", streamID, err)
	}
}

// drain runs bounded batches until one changes nothing.
func (f *retentionStateFixture) drain(t *testing.T, options models.AgentDeliveryRetentionBatchOptions) models.AgentDeliveryRetentionBatchResult {
	t.Helper()
	var total models.AgentDeliveryRetentionBatchResult
	for attempt := 0; attempt < 50; attempt++ {
		result, err := f.repo.RunAgentDeliveryRetentionBatch(context.Background(), options)
		if err != nil {
			t.Fatalf("retention batch %d: %v", attempt+1, err)
		}
		total.EventPayloadsPruned += result.EventPayloadsPruned
		total.EventRowsRetired += result.EventRowsRetired
		if result.EventPayloadsPruned == 0 && result.EventRowsRetired == 0 && !result.DiscoveryAdvanced {
			return total
		}
	}
	return total
}

func (f *retentionStateFixture) retired(t *testing.T, streamID string) int64 {
	t.Helper()
	cursor, err := f.repo.GetAgentDeliveryCursor(context.Background(), streamID)
	if err != nil {
		t.Fatalf("read cursor %s: %v", streamID, err)
	}
	return cursor.RetiredSequence
}

func (f *retentionStateFixture) rawRows(t *testing.T, streamID string) int {
	t.Helper()
	var rows int
	if err := f.repo.db.QueryRowx(f.repo.db.Rebind(`
		SELECT COUNT(*) FROM agent_delivery_inbox WHERE stream_id = ? AND payload_pruned_at IS NULL`), streamID).Scan(&rows); err != nil {
		t.Fatalf("count raw rows %s: %v", streamID, err)
	}
	return rows
}

func retentionGenericEffect(event *models.AgentDeliveryEvent, at time.Time) *models.AgentDeliveryEffect {
	completedAt := at
	return &models.AgentDeliveryEffect{
		EffectKey: "agent_delivery.event:" + event.StreamID + ":" + strconv.FormatInt(event.Sequence, 10),
		StreamID:  event.StreamID, Sequence: event.Sequence, EffectType: "agent_delivery.event",
		State: models.DeliveryEffectCompleted, CreatedAt: at, CompletedAt: &completedAt,
	}
}

func TestDeliveryRetentionRetiresProjectedSessionStateEvents(t *testing.T) {
	for _, engine := range []string{"SQLite", "Postgres"} {
		t.Run(engine, func(t *testing.T) {
			f := newRetentionStateFixture(t, engine)
			const sessionID, streamID = "state-retire-session", "state-retire-stream"
			f.session(t, "state-retire-task", sessionID, models.TaskSessionStateWaitingForInput, true, f.now)
			events := f.stream(t, sessionID, streamID, retentionRealStreamTypes)
			last := int64(len(events))

			// Past the payload grace but inside the replay window: every
			// projected row, state events included, compacts and nothing retires.
			f.now = f.baseNow.Add(2 * time.Hour)
			compacted := f.drain(t, models.AgentDeliveryRetentionBatchOptions{})
			if compacted.EventPayloadsPruned != len(events) || compacted.EventRowsRetired != 0 || f.retired(t, streamID) != 0 {
				t.Fatalf("grace compaction = %+v retired=%d, want %d payloads pruned and no retirement", compacted, f.retired(t, streamID), len(events))
			}
			if raw := f.rawRows(t, streamID); raw != 0 {
				t.Fatalf("%d projected rows kept raw payloads after grace", raw)
			}

			// Past the replay window the whole stream retires from agent_capabilities on.
			f.now = f.baseNow.Add(25 * time.Hour)
			retired := f.drain(t, models.AgentDeliveryRetentionBatchOptions{})
			if retired.EventRowsRetired != len(events) || f.retired(t, streamID) != last {
				t.Fatalf("replay-expired retirement = %+v checkpoint=%d, want all %d rows", retired, f.retired(t, streamID), last)
			}
			assertRetentionRowCount(t, f.repo, "agent_delivery_inbox", "stream_id = ?", streamID, 0)
			assertRetentionRowCount(t, f.repo, "agent_delivery_effects", "stream_id = ? AND effect_type = 'agent_delivery.event'", streamID, 0)

			replay := *events[0]
			if inserted, err := f.repo.ReceiveAgentDeliveryEvent(context.Background(), &replay, last); !errors.Is(err, repoerrors.ErrAgentDeliveryReplayExpired) || inserted {
				t.Fatalf("replayed retired agent_capabilities = %v, %v; want typed expiry", inserted, err)
			}
			var canonical int
			if err := f.repo.db.QueryRowx(f.repo.db.Rebind(`
				SELECT COUNT(*) FROM task_session_messages WHERE task_session_id = ?`), sessionID).Scan(&canonical); err != nil {
				t.Fatalf("count canonical messages: %v", err)
			}
			if canonical == 0 {
				t.Fatal("retirement removed canonical conversation history")
			}
		})
	}
}

func TestDeliveryRetentionUnknownEventTypeStillStopsRetirement(t *testing.T) {
	f := newRetentionStateFixture(t, "SQLite")
	const sessionID, streamID = "state-unknown-session", "state-unknown-stream"
	f.session(t, "state-unknown-task", sessionID, models.TaskSessionStateWaitingForInput, true, f.now)
	f.stream(t, sessionID, streamID, []string{
		streams.EventTypeAgentCapabilities, streams.EventTypeMessageChunk, unknownRetentionEventType, streams.EventTypeMessageChunk,
	})
	f.now = f.baseNow.Add(25 * time.Hour)
	f.drain(t, models.AgentDeliveryRetentionBatchOptions{})
	if got := f.retired(t, streamID); got != 2 {
		t.Fatalf("checkpoint = %d, want retirement to stop before the unclassified event at 3", got)
	}
	var payload []byte
	if err := f.repo.db.QueryRowx(f.repo.db.Rebind(`
		SELECT payload FROM agent_delivery_inbox WHERE stream_id = ? AND sequence = 3`), streamID).Scan(&payload); err != nil || len(payload) == 0 {
		t.Fatalf("unclassified event lost its payload: %d bytes, %v", len(payload), err)
	}
}

// Open recovery blocks stay open; they stop protecting delivery rows only when
// their session can no longer be recovered as the task's current conversation.
func TestDeliveryRetentionStaleRecoveryBlocksReleaseEvidence(t *testing.T) {
	for _, engine := range []string{"SQLite", "Postgres"} {
		t.Run(engine, func(t *testing.T) {
			f := newRetentionStateFixture(t, engine)
			earlier := f.baseNow.Add(-time.Hour)
			cases := []struct {
				name      string
				state     models.TaskSessionState
				primary   bool
				successor bool
				protected bool
			}{
				{name: "live-primary", state: models.TaskSessionStateWaitingForInput, primary: true, protected: true},
				{name: "live-parallel", state: models.TaskSessionStateRunning, protected: true},
				{name: "failed", state: models.TaskSessionStateFailed},
				{name: "cancelled-primary", state: models.TaskSessionStateCancelled, primary: true},
				{name: "completed", state: models.TaskSessionStateCompleted},
				{name: "superseded", state: models.TaskSessionStateWaitingForInput, successor: true},
			}
			types := []string{streams.EventTypeAgentCapabilities, streams.EventTypeMessageChunk, streams.EventTypeToolCall, streams.EventTypeMessageChunk}
			for _, tc := range cases {
				taskID, sessionID := "block-task-"+tc.name, "block-session-"+tc.name
				f.session(t, taskID, sessionID, tc.state, tc.primary, earlier)
				if tc.successor {
					f.session(t, taskID, "block-successor-"+tc.name, models.TaskSessionStateWaitingForInput, true, f.baseNow)
				}
				f.stream(t, sessionID, "block-stream-"+tc.name, types)
				if err := f.repo.UpsertSessionRecoveryBlock(context.Background(), &models.SessionRecoveryBlock{
					ID: "block-" + tc.name, SessionID: sessionID, IncarnationID: "incarnation-" + sessionID,
					ExpectedGeneration: 1, Reason: "unknown_prompt_outcome", State: models.RecoveryBlockOpen,
					ConsumerReference: "agent_delivery",
				}); err != nil {
					t.Fatalf("open block %s: %v", tc.name, err)
				}
			}
			f.now = f.baseNow.Add(25 * time.Hour)
			f.drain(t, models.AgentDeliveryRetentionBatchOptions{})
			for _, tc := range cases {
				streamID := "block-stream-" + tc.name
				retired, raw := f.retired(t, streamID), f.rawRows(t, streamID)
				if tc.protected && (retired != 0 || raw != len(types)) {
					t.Errorf("%s: live block lost evidence: retired=%d raw=%d", tc.name, retired, raw)
				}
				if !tc.protected && retired != int64(len(types)) {
					t.Errorf("%s: stale block still pins the stream: retired=%d raw=%d", tc.name, retired, raw)
				}
				block, err := f.repo.GetSessionRecoveryBlock(context.Background(), "block-"+tc.name)
				if err != nil || block == nil || block.State != models.RecoveryBlockOpen {
					t.Errorf("%s: retention changed the admission fence: %+v, %v", tc.name, block, err)
				}
			}
		})
	}
}

// Retirement discovery must reach an expired head even when other streams
// hold large compacted replay windows that cannot retire yet.
func TestDeliveryRetentionRetirementDiscoverySkipsReplayWindows(t *testing.T) {
	f := newRetentionStateFixture(t, "SQLite")
	f.session(t, "discovery-task", "discovery-session", models.TaskSessionStateWaitingForInput, true, f.now)
	windowTypes := make([]string, 60)
	for index := range windowTypes {
		windowTypes[index] = streams.EventTypeMessageChunk
	}
	f.stream(t, "discovery-session", "discovery-a-window", windowTypes)
	f.stream(t, "discovery-session", "discovery-b-expired", []string{
		streams.EventTypeAgentCapabilities, streams.EventTypeSessionMode, streams.EventTypeMessageChunk,
	})
	// Compact both streams inside their replay windows.
	f.now = f.baseNow.Add(2 * time.Hour)
	f.drain(t, models.AgentDeliveryRetentionBatchOptions{})
	if f.rawRows(t, "discovery-a-window")+f.rawRows(t, "discovery-b-expired") != 0 || f.retired(t, "discovery-b-expired") != 0 {
		t.Fatal("fixture did not compact both streams before the replay window expired")
	}
	// Only stream b leaves its window; stream a was projected a day later.
	f.ageProjection(t, "discovery-a-window", f.baseNow.Add(24*time.Hour))
	f.now = f.baseNow.Add(26 * time.Hour)
	result, err := f.repo.RunAgentDeliveryRetentionBatch(context.Background(), models.AgentDeliveryRetentionBatchOptions{MaxCandidates: 10})
	if err != nil {
		t.Fatalf("bounded retention batch: %v", err)
	}
	if result.EventRowsRetired != 3 || f.retired(t, "discovery-b-expired") != 3 {
		t.Fatalf("first bounded batch = %+v checkpoint=%d; a compacted replay window starved the expired head",
			result, f.retired(t, "discovery-b-expired"))
	}
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := f.repo.RunAgentDeliveryRetentionBatch(context.Background(), models.AgentDeliveryRetentionBatchOptions{MaxCandidates: 10}); err != nil {
			t.Fatalf("idle retention batch %d: %v", attempt+1, err)
		}
	}
	if got := f.retired(t, "discovery-a-window"); got != 0 {
		t.Fatalf("replay window retired early: checkpoint=%d", got)
	}
}
