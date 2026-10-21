package backendapp

import (
	"context"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

type stubProfileUsage struct {
	usage *agentusage.ProviderUsage
}

func (s stubProfileUsage) GetUsage(context.Context, string) (*agentusage.ProviderUsage, error) {
	return s.usage, nil
}

type stubProviderLimits []dynamicruntime.ProviderLimit

func (s stubProviderLimits) ListProviderLimits(context.Context) ([]dynamicruntime.ProviderLimit, error) {
	return s, nil
}

func TestLimitCalendarPrefersObservedUsage(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	weekly := now.Add(39 * time.Hour)
	monthly := now.Add(26 * 24 * time.Hour)
	usage := stubProfileUsage{usage: &agentusage.ProviderUsage{Windows: []agentusage.UtilizationWindow{
		{Label: "rolling", UtilizationPct: 10, ResetAt: now.Add(3 * time.Hour), DurationSeconds: 5 * 3600},
		{Label: "weekly", UtilizationPct: 100, ResetAt: weekly, DurationSeconds: 7 * 24 * 3600},
		{Label: "monthly", UtilizationPct: 50, ResetAt: monthly},
		{Label: "weekly", UtilizationPct: 100, ResetAt: now.Add(90 * time.Hour), ModelID: "opencode-go/other"},
	}}}
	manual := now.Add(10 * 24 * time.Hour)
	calendar := newDynamicLimitCalendar(usage, stubProviderLimits{{Provider: "opencode-go", MonthlyResetAt: &manual}})
	candidate := dynamicruntime.Candidate{ID: "profile", ModelID: "opencode-go/kimi-k2"}

	if got, ok := calendar.ExhaustedUntil(context.Background(), candidate, now); !ok || !got.Equal(weekly) {
		t.Fatalf("ExhaustedUntil = %s, %v; want %s", got, ok, weekly)
	}
	if got, ok := calendar.MonthlyReset(context.Background(), candidate, now); !ok || !got.Equal(monthly) {
		t.Fatalf("MonthlyReset = %s, %v; want observed %s", got, ok, monthly)
	}
}

func TestOpenRouterFreeDailyQuotaWaitsForUTCMidnight(t *testing.T) {
	// OpenRouter counts free requests per UTC day, so its daily quota is rebuilt
	// at the next UTC midnight. The provider's own window reset can land a
	// fraction of a second earlier, which would resume the model against a quota
	// that is still spent.
	for _, now := range []time.Time{
		time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 4, 0, 0, 0, 1, time.UTC),
		time.Date(2026, 10, 4, 23, 59, 59, 999999999, time.UTC),
		// A non-UTC host clock must still land on the UTC day boundary.
		time.Date(2026, 10, 4, 9, 30, 0, 0, time.FixedZone("KST", 9*3600)),
	} {
		usage := stubProfileUsage{usage: &agentusage.ProviderUsage{Windows: []agentusage.UtilizationWindow{
			{Label: "daily", UtilizationPct: 100, ResetAt: now.Add(time.Minute), DurationSeconds: 24 * 3600},
		}}}
		calendar := newDynamicLimitCalendar(usage, nil)
		candidate := dynamicruntime.Candidate{ID: "profile", ModelID: "openrouter/qwen/qwen3-coder:free"}

		want := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
		if got, ok := calendar.ExhaustedUntil(context.Background(), candidate, now); !ok || !got.Equal(want) {
			t.Fatalf("now=%s ExhaustedUntil = %s, %v; want %s", now, got, ok, want)
		}
	}
}

func TestOpenRouterDailyWindowNeedsAnExhaustedDailyQuota(t *testing.T) {
	now := time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC)
	// A daily window that is not exhausted says nothing about a daily limit, so
	// it must leave the caller on its ordinary path instead of shortening the
	// block to the next midnight.
	usage := stubProfileUsage{usage: &agentusage.ProviderUsage{Windows: []agentusage.UtilizationWindow{
		{Label: "daily", UtilizationPct: 40, ResetAt: now.Add(18 * time.Hour), DurationSeconds: 24 * 3600},
	}}}
	calendar := newDynamicLimitCalendar(usage, nil)

	free := dynamicruntime.Candidate{ID: "profile", ModelID: "openrouter/qwen/qwen3-coder:free"}
	if _, ok := calendar.ExhaustedUntil(context.Background(), free, now); ok {
		t.Fatal("an unexhausted daily window must not report an exhausted quota")
	}

	// A paid OpenRouter model has no per-UTC-day quota, so even a consumed daily
	// window keeps the provider's own reset.
	paidUsage := stubProfileUsage{usage: &agentusage.ProviderUsage{Windows: []agentusage.UtilizationWindow{
		{Label: "daily", UtilizationPct: 100, ResetAt: now.Add(18 * time.Hour), DurationSeconds: 24 * 3600},
	}}}
	paidCalendar := newDynamicLimitCalendar(paidUsage, nil)
	paid := dynamicruntime.Candidate{ID: "profile", ModelID: "openrouter/anthropic/claude-sonnet"}
	if got, ok := paidCalendar.ExhaustedUntil(context.Background(), paid, now); !ok || !got.Equal(now.Add(18*time.Hour)) {
		t.Fatalf("paid ExhaustedUntil = %s, %v; want the window's own reset", got, ok)
	}
}

func TestLimitCalendarFallsBackToOperatorMonthlyReset(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
	calendar := newDynamicLimitCalendar(stubProfileUsage{}, stubProviderLimits{{Provider: "opencode-go", MonthlyResetAt: &anchor}})
	candidate := dynamicruntime.Candidate{ID: "profile", ModelID: "opencode-go/kimi-k2"}

	want := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	if got, ok := calendar.MonthlyReset(context.Background(), candidate, now); !ok || !got.Equal(want) {
		t.Fatalf("MonthlyReset = %s, %v; want %s", got, ok, want)
	}
	if _, ok := calendar.ExhaustedUntil(context.Background(), candidate, now); ok {
		t.Fatal("no observed usage must not report an exhausted window")
	}
	remote := candidate
	remote.RemoteExecution = true
	observed := newDynamicLimitCalendar(stubProfileUsage{usage: &agentusage.ProviderUsage{Windows: []agentusage.UtilizationWindow{
		{Label: "weekly", UtilizationPct: 100, ResetAt: now.Add(time.Hour)},
	}}}, nil)
	if _, ok := observed.ExhaustedUntil(context.Background(), remote, now); ok {
		t.Fatal("a remote execution must not borrow the host account's usage")
	}
}
