package sqlite

import (
	"context"
	"testing"
	"time"
)

func insertRouteAttempt(t *testing.T, repo *Repository, id, sessionID, logical, candidate string, generation int64, at time.Time) {
	t.Helper()
	if _, err := repo.db.Exec(repo.db.Rebind(`
		INSERT INTO dynamic_route_attempts
			(id, session_id, logical_profile_id, execution_profile_id, route_generation, profile_version, reason, created_at)
		VALUES (?, ?, ?, ?, ?, 1, 'tier_round_robin', ?)
	`), id, sessionID, logical, candidate, generation, at); err != nil {
		t.Fatalf("insert attempt %s: %v", id, err)
	}
}

// TestLastDynamicRouteSelectionsKeepsTheLatestPerCandidate pins the durable
// source round-robin selection continues from after a restart.
func TestLastDynamicRouteSelectionsKeepsTheLatestPerCandidate(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	createUsageEventsTestTask(t, repo, "task-rr")
	createUsageEventsTestSession(t, repo, "session-rr", "task-rr")
	base := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	insertRouteAttempt(t, repo, "a1", "session-rr", "dynamic", "a", 1, base)
	insertRouteAttempt(t, repo, "b1", "session-rr", "dynamic", "b", 2, base.Add(time.Minute))
	insertRouteAttempt(t, repo, "a2", "session-rr", "dynamic", "a", 3, base.Add(2*time.Minute))
	insertRouteAttempt(t, repo, "x1", "session-rr", "other", "x", 4, base.Add(3*time.Minute))

	latest, err := repo.LastDynamicRouteSelections(context.Background(), "dynamic")
	if err != nil {
		t.Fatalf("LastDynamicRouteSelections: %v", err)
	}
	if len(latest) != 2 || !latest["a"].Equal(base.Add(2*time.Minute)) || !latest["b"].Equal(base.Add(time.Minute)) {
		t.Fatalf("latest = %v, want the newest attempt per candidate of this profile", latest)
	}
}

func TestUsageLimitObservationsRoundTrip(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	for index, turns := range []int64{120, 80} {
		if err := repo.InsertUsageLimitObservation(ctx, UsageLimitObservation{
			ID: []string{"o1", "o2"}[index], ExecutionProfileID: "zen", AccountKey: "opencode-zen:auth",
			Code: "quota_limited", ObservedAt: at.Add(time.Duration(index) * time.Hour), TurnsDay: turns,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	observations, err := repo.ListUsageLimitObservationsSince(ctx, at.Add(-time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(observations) != 2 || observations[0].ID != "o2" || observations[1].TurnsDay != 120 {
		t.Fatalf("observations = %#v, want both, newest first", observations)
	}
	if later, _ := repo.ListUsageLimitObservationsSince(ctx, at.Add(2*time.Hour)); len(later) != 0 {
		t.Fatalf("later = %#v, want none after the lookback start", later)
	}
}
