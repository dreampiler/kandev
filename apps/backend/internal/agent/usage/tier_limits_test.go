package usage

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func TestProviderTierOwnerOverrideKeepsItsFullAllowance(t *testing.T) {
	defaults := TierLimits{RequestsPerDay: limitNumber(100), DailyReset: "00:00 UTC"}
	learned := TierLimits{RequestsPerDay: limitNumber(80), Learned: true}
	owner := TierLimits{RequestsPerDay: limitNumber(90)}
	effective := MergeTierLimits(MergeTierLimits(defaults, learned), owner)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	if got := EvaluateTierLimits(effective, LimitUsage{RequestsDay: 89}, now); got.Reason != "" {
		t.Fatalf("owner allowance was reduced by the learned reserve: %+v", got)
	}
	got := EvaluateTierLimits(effective, LimitUsage{RequestsDay: 90}, now)
	if got.Reason != "limit_daily" || !got.RetryAt.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily admission = %+v", got)
	}
	effective = MergeTierLimits(effective, TierLimits{RequestsPerDay: limitNumber(0)})
	if got := EvaluateTierLimits(effective, LimitUsage{RequestsDay: 1000}, now); got.Reason != "" {
		t.Fatalf("explicit zero must disable the local cap: %+v", got)
	}
}

func TestProviderTierMergePreservesWindowLearning(t *testing.T) {
	now := time.Now()
	learned := TierLimits{Windows: []LimitBudget{{Window: "5h", Unit: "tokens", Value: limitNumber(100), Reset: "rolling", Learned: true}}}
	effective := MergeTierLimits(TierLimits{}, learned)
	if !effective.Windows[0].Learned {
		t.Fatal("merging a window discarded its learned provenance")
	}
	if got := EvaluateTierLimits(effective, LimitUsage{Windows: map[string]float64{"5h": 95}}, now); got.Reason != "limit_window" {
		t.Fatalf("learned window lost its reserve: %+v", got)
	}
	owner := TierLimits{Windows: []LimitBudget{{Window: "5h", Unit: "tokens", Value: limitNumber(100), Reset: "rolling"}}}
	effective = MergeTierLimits(effective, owner)
	if got := EvaluateTierLimits(effective, LimitUsage{Windows: map[string]float64{"5h": 95}}, now); got.Reason != "" || effective.Windows[0].Learned {
		t.Fatalf("owner window must replace learned provenance and reserve: %+v", got)
	}
}

func TestProviderTierWindowRulesInheritAllowance(t *testing.T) {
	base := TierLimits{Windows: []LimitBudget{{Window: "5h", Unit: "tokens", Value: limitNumber(100), Reset: "rolling", Learned: true}}}
	owner := TierLimits{Windows: []LimitBudget{{Window: "5h", Unit: "tokens", Reset: "utc"}}}
	effective := MergeTierLimits(base, owner)
	budget := effective.Windows[0]
	if budget.Value == nil || *budget.Value != 100 || !budget.Learned || budget.Reset != "utc" {
		t.Fatalf("rule-only owner edit discarded inherited allowance: %+v", budget)
	}
}

func TestProviderTierUnknownLearningRetainsWindowUncertainty(t *testing.T) {
	used := LimitUsage{RequestsDay: 80, Windows: map[string]float64{"5h": 500}}
	learned := LearnTierLimits(TierLimits{}, "daily", used)
	if learned.RequestsPerDay == nil || *learned.RequestsPerDay != 80 || !learned.Learned {
		t.Fatalf("daily learned allowance = %+v", learned)
	}
	known := LearnTierLimits(TierLimits{RequestsPerDay: limitNumber(100)}, "daily", used)
	if *known.RequestsPerDay != 100 || known.Learned {
		t.Fatalf("PR1 must retain a known default: %+v", known)
	}
	provisional := LearnTierLimits(TierLimits{}, "unknown_subscription", used)
	if len(provisional.Windows) != 1 || provisional.Windows[0].Window != "5h" || !provisional.Windows[0].WindowEstimated {
		t.Fatalf("ambiguous subscription evidence = %+v", provisional)
	}
}

func TestProviderTierResetUsesTheOwnerTimeZone(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	start, reset, err := DailyLimitWindow(now, "05:00 Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	if !start.Equal(time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC)) || !reset.Equal(start.Add(24*time.Hour)) {
		t.Fatalf("window = %v..%v", start, reset)
	}
	if err := ValidateTierLimits(TierLimits{DailyReset: "05:00 Invalid/Zone"}); err == nil {
		t.Fatal("invalid owner reset must be rejected")
	}
}

func TestApprovedInstallationDefaultsDoNotRequireAUsageClient(t *testing.T) {
	openRouter := DefaultTierLimits("openrouter", TierFree, "", false)
	if openRouter.RequestsPerDay == nil || *openRouter.RequestsPerDay != 1000 ||
		openRouter.DailyReset != "00:00 UTC" || openRouter.Provenance["requests_per_day"] != ProvenanceMeasured {
		t.Fatalf("openrouter free defaults = %#v", openRouter)
	}
	gateway := DefaultTierLimits("llmgateway", TierFree, "", false)
	if gateway.RequestsPerMinute == nil || *gateway.RequestsPerMinute != 20 ||
		gateway.RequestsPerWindow != nil || !gateway.RequestsPerModel ||
		gateway.Provenance["requests_per_minute"] != ProvenanceMeasured {
		t.Fatalf("llmgateway free defaults = %#v", gateway)
	}
	devPass := DefaultTierLimits("llmgateway", TierSubscription, "devpass", false)
	if devPass.RequestsPerMinute == nil || *devPass.RequestsPerMinute != 120 ||
		devPass.MaxConcurrentTurns == nil || *devPass.MaxConcurrentTurns != 50 {
		t.Fatalf("devpass defaults = %#v", devPass)
	}
}
