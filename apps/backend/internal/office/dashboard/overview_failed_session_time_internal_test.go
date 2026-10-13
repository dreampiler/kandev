package dashboard

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// TestFailedSessionEventsUseTheFailureTime pins the 24-hour feed to when a
// session failed. A step move rewrites every session row of its task, so an
// old FAILED session can carry a fresh updated_at; it must not reappear as a
// new failure, and a recent failure is reported at its completion time.
func TestFailedSessionEventsUseTheFailureTime(t *testing.T) {
	now := time.Date(2026, 10, 12, 14, 45, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	touched := now.Add(-3 * time.Minute)
	old := &sqlite.OverviewSessionRow{
		ID: "old-failed", TaskID: "task", State: sessionStateFailed,
		StartedAt: now.Add(-72 * time.Hour), CompletedAt: now.Add(-70 * time.Hour), UpdatedAt: touched,
		ErrorMessage: "fatal: worktree already exists",
	}
	recent := &sqlite.OverviewSessionRow{
		ID: "recent-failed", TaskID: "task", State: sessionStateFailed,
		StartedAt: now.Add(-3 * time.Hour), CompletedAt: now.Add(-2 * time.Hour), UpdatedAt: touched,
		ErrorMessage: "agent stopped responding",
	}
	snap := &overviewSnapshot{
		now: now,
		tasks: []*overviewTask{{
			row:      &sqlite.OverviewTaskRow{ID: "task", WorkspaceID: "ws", Title: "Task"},
			sessions: []*sqlite.OverviewSessionRow{recent, old},
		}},
	}

	events := failedSessionEvents(snap, since)
	if len(events) != 1 {
		t.Fatalf("events = %d, want only the failure inside the window", len(events))
	}
	if events[0].SessionID != recent.ID {
		t.Fatalf("event session = %q, want %q", events[0].SessionID, recent.ID)
	}
	if !events[0].At.Equal(recent.CompletedAt) {
		t.Fatalf("event time = %v, want the failure time %v", events[0].At, recent.CompletedAt)
	}
}
