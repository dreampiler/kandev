package routingerr

import (
	"testing"
	"time"
)

const devPassCreditNotice = "AI_APICallError: Dev Plan credit limit reached. Upgrade your plan or wait for renewal on 10/7/2026. " +
	"Or enable pay-as-you-go overflow in your DevPass dashboard to keep going past your allowance"

func TestClassify_CreditNoticeCarriesItsRenewalDay(t *testing.T) {
	resetInjection()
	observed := time.Date(2026, 10, 7, 23, 22, 0, 0, time.FixedZone("KST", 9*60*60))
	got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: devPassCreditNotice, OccurredAt: observed})
	if got.Code != CodeQuotaLimited || got.ClassifierRule != "opencode.stderr.credit.v1" {
		t.Fatalf("classification = %s (%s), want quota_limited via the credit rule", got.Code, got.ClassifierRule)
	}
	want := time.Date(2026, 10, 7, 0, 0, 0, 0, time.FixedZone("UTC+14", 14*60*60))
	if got.RenewalAt == nil || !got.RenewalAt.Equal(want) {
		t.Fatalf("RenewalAt = %v, want the earliest start of 2026-10-07 (%s)", got.RenewalAt, want)
	}
	if !got.RenewalAt.Before(observed) {
		t.Fatalf("a renewal day of today must already have begun at %s, got %s", observed, got.RenewalAt)
	}
}

func TestParseRenewalDayStart(t *testing.T) {
	zone := time.FixedZone("UTC+14", 14*60*60)
	for text, want := range map[string]time.Time{
		"wait for renewal on 10/08/2026": time.Date(2026, 10, 8, 0, 0, 0, 0, zone),
		"plan renews on 2026-11-01.":     time.Date(2026, 11, 1, 0, 0, 0, 0, zone),
		"Renewal on 1/31/2027":           time.Date(2027, 1, 31, 0, 0, 0, 0, zone),
	} {
		got := parseRenewalDayStart(text)
		if got == nil || !got.Equal(want) {
			t.Fatalf("%q → %v, want %s", text, got, want)
		}
	}
	for _, text := range []string{"renewal on 13/01/2026", "renewal on 2/30/2026", "renewal soon", "blocked until 10/7/2026"} {
		if got := parseRenewalDayStart(text); got != nil {
			t.Fatalf("%q → %s, want nil", text, got)
		}
	}
}

func TestClassify_RenewalDayIsOnlyReadForQuotaFailures(t *testing.T) {
	resetInjection()
	got := Classify(Input{Phase: PhasePromptSend, ProviderID: "opencode-acp", Stderr: "AI_APICallError: Rate limit exceeded. renewal on 10/7/2026"})
	if got.Code != CodeRateLimited || got.RenewalAt != nil {
		t.Fatalf("classification = %s RenewalAt=%v, want rate_limited without a renewal day", got.Code, got.RenewalAt)
	}
}
