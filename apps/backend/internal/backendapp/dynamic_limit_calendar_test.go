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
