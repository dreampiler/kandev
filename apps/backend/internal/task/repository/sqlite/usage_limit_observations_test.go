package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestUsageLimitObservationsRoundTrip(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	for index, turns := range []int64{120, 80} {
		if err := repo.InsertUsageLimitObservation(ctx, UsageLimitObservation{
			ID: []string{"o1", "o2"}[index], ExecutionProfileID: "zen", AccountKey: "opencode-zen:auth",
			Code: "quota_limited", ObservedAt: at.Add(time.Duration(index) * time.Hour), TurnsDay: turns,
			Tier: "free", Dimension: "daily", RequestsDay: 80, WindowUsageJSON: `{"day":80}`,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if err := repo.runMigrations(ctx); err != nil {
		t.Fatalf("replay observation migrations: %v", err)
	}
	observations, err := repo.ListUsageLimitObservationsSince(ctx, at.Add(-time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(observations) != 2 || observations[0].ID != "o2" || observations[1].TurnsDay != 120 {
		t.Fatalf("observations = %#v, want both, newest first", observations)
	}
	if observations[0].Tier != "free" || observations[0].Dimension != "daily" || observations[0].RequestsDay != 80 || observations[0].WindowUsageJSON != `{"day":80}` {
		t.Fatalf("fresh/replayed schema lost tier evidence: %+v", observations[0])
	}
	if later, _ := repo.ListUsageLimitObservationsSince(ctx, at.Add(2*time.Hour)); len(later) != 0 {
		t.Fatalf("later = %#v, want none after the lookback start", later)
	}
}

func TestUsageLimitObservationsLegacyUpgrade(t *testing.T) {
	repo := newUsageEventsTestRepo(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	if err := repo.InsertUsageLimitObservation(ctx, UsageLimitObservation{
		ID: "legacy-observation", ExecutionProfileID: "zen", ObservedAt: at, TurnsDay: 80,
	}); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"tier", "dimension", "requests_minute", "requests_day", "active_turns", "window_usage_json"} {
		if _, err := repo.db.ExecContext(ctx, "ALTER TABLE usage_limit_observations DROP COLUMN "+column); err != nil {
			t.Fatalf("restore legacy observation schema: %v", err)
		}
	}
	if err := repo.initSchema(); err != nil {
		t.Fatalf("upgrade existing observation table: %v", err)
	}
	observations, err := repo.ListUsageLimitObservationsSince(ctx, at.Add(-time.Hour))
	if err != nil || len(observations) != 1 {
		t.Fatalf("upgraded observation read = %+v, %v", observations, err)
	}
	got := observations[0]
	if got.ID != "legacy-observation" || got.TurnsDay != 80 || got.Tier != "" || got.RequestsDay != 0 || got.WindowUsageJSON != "{}" {
		t.Fatalf("upgrade lost legacy observation or column defaults: %+v", got)
	}
}
