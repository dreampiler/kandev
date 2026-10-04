package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// listingWriter is a fake ExecutorRunningWriter that also implements the
// optional executorRunningLister capability, so a test can pin
// Manager.ListLiveStandaloneExecutorsRunning's delegation without a real DB.
type listingWriter struct {
	invariantWriter
	rows []*models.ExecutorRunning
	err  error
}

func (w *listingWriter) ListExecutorsRunningLiveStandalone(context.Context) ([]*models.ExecutorRunning, error) {
	return w.rows, w.err
}

// TestListLiveStandaloneExecutorsRunningDelegatesToWriter pins that the
// manager forwards to the writer's optional lister capability when present.
// This inventory is read at startup step 3, before any control-server
// contact, to build the recovery guard/correlation set (discovery H).
func TestListLiveStandaloneExecutorsRunningDelegatesToWriter(t *testing.T) {
	mgr := newTestManager(t)
	want := []*models.ExecutorRunning{{SessionID: "session-1"}}
	mgr.SetExecutorRunningWriter(&listingWriter{rows: want})

	got, err := mgr.ListLiveStandaloneExecutorsRunning(context.Background())
	if err != nil {
		t.Fatalf("ListLiveStandaloneExecutorsRunning: %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "session-1" {
		t.Fatalf("got = %#v, want %#v", got, want)
	}
}

// TestListLiveStandaloneExecutorsRunningWithoutListerReturnsEmpty pins the
// best-effort fallback: a writer that doesn't support listing (e.g. a test
// double) yields no candidates rather than an error, matching the rest of
// this file's optional-interface pattern.
func TestListLiveStandaloneExecutorsRunningWithoutListerReturnsEmpty(t *testing.T) {
	mgr := newTestManager(t)
	mgr.SetExecutorRunningWriter(&invariantWriter{})

	got, err := mgr.ListLiveStandaloneExecutorsRunning(context.Background())
	if err != nil {
		t.Fatalf("ListLiveStandaloneExecutorsRunning: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %#v, want empty", got)
	}
}

// terminalStateSessionReader is an ExecutorProfileReader that reports a fixed
// state per session, so the recovery inventory's terminal-session filter can be
// pinned without a real DB.
type terminalStateSessionReader struct {
	ExecutorProfileReader
	states map[string]models.TaskSessionState
	err    error
}

func (r *terminalStateSessionReader) GetTaskSession(_ context.Context, id string) (*models.TaskSession, error) {
	if r.err != nil {
		return nil, r.err
	}
	state, ok := r.states[id]
	if !ok {
		return nil, models.ErrTaskSessionNotFound
	}
	return &models.TaskSession{ID: id, State: state}, nil
}

// TestListLiveStandaloneExecutorsRunningDropsTerminalSessions pins that a row
// whose owning session is already terminal is not offered as a recovery
// candidate. Recovery correlation re-tracks any live instance that still has a
// record, so keeping the record re-adopts the runtime for a session that can no
// longer receive a message. Dropping it makes the instance an orphan, which the
// correlation already stops.
func TestListLiveStandaloneExecutorsRunningDropsTerminalSessions(t *testing.T) {
	rows := []*models.ExecutorRunning{
		{SessionID: "s-running"},
		{SessionID: "s-failed"},
		{SessionID: "s-cancelled"},
		{SessionID: "s-completed"},
		{SessionID: "s-waiting"},
		{SessionID: "s-idle"},
	}
	mgr := newTestManager(t)
	mgr.SetExecutorRunningWriter(&listingWriter{rows: rows})
	mgr.SetExecutorProfileReader(&terminalStateSessionReader{states: map[string]models.TaskSessionState{
		"s-running":   models.TaskSessionStateRunning,
		"s-failed":    models.TaskSessionStateFailed,
		"s-cancelled": models.TaskSessionStateCancelled,
		"s-completed": models.TaskSessionStateCompleted,
		"s-waiting":   models.TaskSessionStateWaitingForInput,
		"s-idle":      models.TaskSessionStateIdle,
	}})

	got, err := mgr.ListLiveStandaloneExecutorsRunning(context.Background())
	if err != nil {
		t.Fatalf("ListLiveStandaloneExecutorsRunning: %v", err)
	}
	gotIDs := make(map[string]bool, len(got))
	for _, rec := range got {
		gotIDs[rec.SessionID] = true
	}
	for _, want := range []string{"s-running", "s-waiting", "s-idle"} {
		if !gotIDs[want] {
			t.Errorf("%s: dropped from the recovery inventory but its session is still open", want)
		}
	}
	for _, unwanted := range []string{"s-failed", "s-cancelled", "s-completed"} {
		if gotIDs[unwanted] {
			t.Errorf("%s: kept in the recovery inventory but its session is terminal", unwanted)
		}
	}
}

// TestListLiveStandaloneExecutorsRunningKeepsRecordWhenSessionUnreadable pins
// the fail-closed direction: an unreadable session is not evidence of a terminal
// state, so its record stays and existing recovery behaviour is unchanged.
func TestListLiveStandaloneExecutorsRunningKeepsRecordWhenSessionUnreadable(t *testing.T) {
	mgr := newTestManager(t)
	mgr.SetExecutorRunningWriter(&listingWriter{
		rows: []*models.ExecutorRunning{{SessionID: "s-unknown"}},
	})
	mgr.SetExecutorProfileReader(&terminalStateSessionReader{
		err: errors.New("session read unavailable"),
	})

	got, err := mgr.ListLiveStandaloneExecutorsRunning(context.Background())
	if err != nil {
		t.Fatalf("ListLiveStandaloneExecutorsRunning: %v", err)
	}
	if len(got) != 1 || got[0].SessionID != "s-unknown" {
		t.Fatalf("unreadable session must keep its record, got %#v", got)
	}
}
