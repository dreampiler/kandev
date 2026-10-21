package sqlite

import (
	"context"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

func TestCircuitFailuresSurviveRestartAfterLaunchClose(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	until := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	for _, snapshot := range []dynamicruntime.CircuitSnapshot{
		{Key: "model:open", State: dynamicruntime.CircuitOpen, Until: until, Code: "quota_limited", Failures: 2},
		{Key: "model:closed-with-strikes", State: dynamicruntime.CircuitClosed, Failures: 3},
		{Key: "model:healthy", State: dynamicruntime.CircuitClosed},
	} {
		if err := repo.SaveCircuit(ctx, snapshot); err != nil {
			t.Fatalf("SaveCircuit(%s): %v", snapshot.Key, err)
		}
	}
	loaded, err := repo.LoadCircuits(ctx)
	if err != nil {
		t.Fatalf("LoadCircuits: %v", err)
	}
	byKey := make(map[string]dynamicruntime.CircuitSnapshot, len(loaded))
	for _, snapshot := range loaded {
		byKey[snapshot.Key] = snapshot
	}
	if len(byKey) != 2 || byKey["model:open"].Failures != 2 || byKey["model:closed-with-strikes"].Failures != 3 {
		t.Fatalf("loaded circuits = %#v", loaded)
	}
	if !byKey["model:open"].Until.Equal(until) {
		t.Fatalf("open until = %s, want %s", byKey["model:open"].Until, until)
	}
}

func TestProviderLimitsRoundTripAndClear(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	monthly := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	block := time.Date(2026, 10, 5, 0, 35, 0, 0, time.UTC)
	if err := repo.SaveProviderLimit(ctx, dynamicruntime.ProviderLimit{
		Provider: "opencode-go", MonthlyResetAt: &monthly, MonthlyResetTimezone: "Asia/Seoul", BlockUntil: &block,
	}); err != nil {
		t.Fatalf("SaveProviderLimit: %v", err)
	}
	limits, err := repo.ListProviderLimits(ctx)
	if err != nil || len(limits) != 1 {
		t.Fatalf("ListProviderLimits = %#v, %v", limits, err)
	}
	got := limits[0]
	if got.Provider != "opencode-go" || got.MonthlyResetTimezone != "Asia/Seoul" ||
		got.MonthlyResetAt == nil || !got.MonthlyResetAt.Equal(monthly) ||
		got.BlockUntil == nil || !got.BlockUntil.Equal(block) {
		t.Fatalf("provider limit = %#v", got)
	}
	if err := repo.SaveProviderLimit(ctx, dynamicruntime.ProviderLimit{Provider: "opencode-go"}); err != nil {
		t.Fatalf("clear SaveProviderLimit: %v", err)
	}
	limits, err = repo.ListProviderLimits(ctx)
	if err != nil || len(limits) != 1 || limits[0].MonthlyResetAt != nil || limits[0].BlockUntil != nil {
		t.Fatalf("cleared provider limit = %#v, %v", limits, err)
	}
}
