package lifecycle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/executor"
	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agentctl/journal"
	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

// @covers AC-EXECUTORS-IDLE-PARKING-001.11
func TestIdleActivityRetainedCompletionPreservesObservationAndDedup(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	for _, known := range []bool{true, false} {
		t.Run(strconv.FormatBool(known), func(t *testing.T) {
			outcome := &agentctl.TurnOutcome{TurnID: 9, Event: agentctl.AgentEvent{Type: "complete", PromptGeneration: 5}}
			if known {
				outcome.Event.ActivityAt = &old
			}
			backend := &fakeTurnOutcomeBackend{MockExecutor: &MockExecutor{name: executor.NameStandalone}}
			mgr, eventBus := newTurnOutcomeTestManager(t, backend)
			backend.eventBus = eventBus
			e := createTestExecution("execution", "task", "session")
			require.NoError(t, mgr.executionStore.Add(e))
			mgr.applyRecoveredTurnOutcome(context.Background(), e,
				&ExecutorInstance{RuntimeName: executor.NameStandalone, StandaloneInstanceID: "instance"}, outcome)
			at, _, _ := e.promptActivitySnapshot()
			if known {
				require.True(t, at.Equal(old))
			} else {
				require.True(t, at.IsZero())
			}
			require.Len(t, backend.ackedCalls, 1)
			require.Greater(t, backend.publishedCountsAtAck[0], 0, "apply must precede ACK")
			require.Equal(t, 1, countEventType(eventBus.PublishedEvents, events.AgentReady))
			live := outcome.Event
			live.Recovered, live.RecoveredActivityAt, live.ControlTurnID = false, nil, outcome.TurnID
			mgr.handleAgentEvent(e, live)
			after, _, _ := e.promptActivitySnapshot()
			require.True(t, after.Equal(at), "redelivery must not change activity")
			require.Equal(t, 1, countEventType(eventBus.PublishedEvents, events.AgentReady))
		})
	}
}

// @covers AC-EXECUTORS-IDLE-PARKING-001.11
func TestIdleActivityDurableReplayUsesJournalTimeWithoutChangingPayload(t *testing.T) {
	ctx := context.Background()
	old := time.Now().UTC().Add(-time.Hour)
	payload, err := json.Marshal(agentctl.AgentEvent{Type: "complete"})
	require.NoError(t, err)
	committed := journal.Event{SessionID: "session", IncarnationID: "incarnation", HarnessGeneration: 2,
		StreamID: "stream", Sequence: 1, Type: "complete", Payload: payload, CreatedAt: old}
	stream := journal.Stream{SessionID: "session", IncarnationID: "incarnation", HarnessGeneration: 2,
		StreamID: "stream", HighWater: 1, FirstRetained: 1}
	repository := &immutableDeliveryRepository{recordingAgentDeliveryRepository: recordingAgentDeliveryRepository{
		cursor: &models.AgentDeliveryCursor{StreamID: "stream"},
	}}
	acknowledged := make(chan uint64, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/delivery/stream/ack" {
			var ack struct {
				Sequence uint64 `json:"sequence"`
			}
			if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			acknowledged <- ack.Sequence
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/api/v1/agent/delivery/stream" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Events []journal.Event `json:"events"`
			Stream journal.Stream  `json:"stream"`
		}{[]journal.Event{committed}, stream})
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	client := agentctl.NewClient(parsed.Hostname(), port, newTestLogger())
	mgr := newTestManager(t)
	e := createTestExecution("execution", "task", "session")
	e.DeliveryMode, e.DeliveryStreamID, e.DeliveryIncarnationID, e.DeliveryHarnessGeneration = DurableDeliveryV1, "stream", "incarnation", 2
	e.DeliveryDescriptor, e.agentctl = &agentctl.DeliveryStatus{Stream: &stream}, client
	require.NoError(t, mgr.executionStore.Add(e))
	sm := NewStreamManager(newTestLogger(), StreamCallbacks{OnAgentEvent: mgr.handleAgentEvent}, nil, nil)
	t.Cleanup(sm.Wait)
	sm.setAgentDeliveryRepository(repository)
	require.NoError(t, sm.ReplayRecoveredDelivery(ctx, e))
	at, _, _ := e.promptActivitySnapshot()
	require.True(t, at.Equal(old), "terminal lifecycle must use journal CreatedAt, never replay time")
	require.Equal(t, uint64(1), e.DeliveryReplayCursor)
	require.Len(t, repository.received, 1)
	require.Len(t, repository.projected, 1)
	var admitted agentctl.AgentEvent
	require.NoError(t, json.Unmarshal(repository.received[0].Payload, &admitted))
	require.False(t, admitted.Recovered)
	require.Nil(t, admitted.RecoveredActivityAt, "replay context must stay outside durable identity")
	select {
	case sequence := <-acknowledged:
		require.Equal(t, uint64(1), sequence)
	case <-time.After(5 * time.Second):
		t.Fatal("projected terminal was not acknowledged")
	}
	require.NoError(t, sm.processRecoveredDeliveryEvent(ctx, e, client, repository, committed, 0, false))
	replayed, _, _ := e.promptActivitySnapshot()
	require.True(t, replayed.Equal(old))
	require.Len(t, repository.projected, 1, "duplicate terminal must not project twice")
}
