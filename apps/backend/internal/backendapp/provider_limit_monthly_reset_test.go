package backendapp

import (
	"context"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

type monthlyResetProviderLimits struct {
	anchor time.Time
}

func (s monthlyResetProviderLimits) ListProviderLimits(context.Context) ([]dynamicruntime.ProviderLimit, error) {
	return []dynamicruntime.ProviderLimit{{Provider: "opencode-go", MonthlyResetAt: &s.anchor}}, nil
}

type noObservedUsage struct{}

func (noObservedUsage) GetUsage(context.Context, string) (*agentusage.ProviderUsage, error) {
	return nil, nil
}

func TestQuotaLimitWithoutProviderResetUsesTheMonthlyReset(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
	calendar := newDynamicLimitCalendar(noObservedUsage{}, monthlyResetProviderLimits{anchor: anchor})
	calendar.timeout = 0
	adapter := newTierLimitAdapter(&usageProviderAdapter{tierLimits: &providerTierLimitService{}}, calendar)
	failure := &routingerr.Error{Code: routingerr.CodeQuotaLimited}
	resolved, scope := adapter.ResolveLimitFailure(context.Background(), dynamicruntime.Candidate{
		ID: "go-kimi", ModelID: "opencode-go/kimi-k2",
	}, failure)
	if resolved == nil || resolved.ResetHint == nil {
		t.Fatalf("resolved = %#v, want the monthly reset hint", resolved)
	}
	if scope != dynamicruntime.SuspensionProvider {
		t.Fatalf("scope = %v, want provider", scope)
	}
	want := time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)
	if !resolved.ResetHint.Equal(want) || !resolved.ResetHint.After(now) {
		t.Fatalf("ResetHint = %s, want %s", resolved.ResetHint, want)
	}
}

func TestFreeModelQuotaLimitDoesNotUseTheMonthlyReset(t *testing.T) {
	anchor := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
	calendar := newDynamicLimitCalendar(noObservedUsage{}, monthlyResetProviderLimits{anchor: anchor})
	calendar.timeout = 0
	adapter := newTierLimitAdapter(&usageProviderAdapter{tierLimits: &providerTierLimitService{}}, calendar)
	resolved, _ := adapter.ResolveLimitFailure(context.Background(), dynamicruntime.Candidate{
		ID: "zen-free", ModelID: "opencode/step-5-preview-free",
	}, &routingerr.Error{Code: routingerr.CodeQuotaLimited})
	if resolved == nil || resolved.ResetHint != nil {
		t.Fatalf("free model got a reset hint: %#v", resolved)
	}
}
