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

func TestOpenRouterZeroRemainingIsExhaustedDespiteAnEarlierUsedCounter(t *testing.T) {
	var raw openRouterKeyResponse
	zero := 0.0
	raw.Data.FreeModelDailyRequests = &struct {
		Used      float64  `json:"used"`
		Limit     float64  `json:"limit"`
		Remaining *float64 `json:"remaining"`
	}{Used: 48, Limit: 50, Remaining: &zero}
	usage := parseOpenRouterUsage(raw, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), 0)
	if len(usage.Windows) != 1 || !usage.Windows[0].LimitReached || usage.FreeModelDailyRequests.Remaining != 0 {
		t.Fatalf("explicit zero remaining was ignored: %+v", usage)
	}
}
