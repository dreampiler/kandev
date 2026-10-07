package dynamic

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// devPassCreditNotice is the exhausted-credit failure DevPass reported a minute
// after its configured monthly reset, while its renewal was still processing.
const devPassCreditNotice = "AI_APICallError: Dev Plan credit limit reached. Upgrade your plan or wait for renewal on 10/7/2026. " +
	"Or enable pay-as-you-go overflow in your DevPass dashboard to keep going past your allowance"

func devPassCandidate() Candidate {
	return Candidate{
		ID: "devpass", Enabled: true, ModelID: "llmgateway/deepseek-v4-pro",
		BindingKey: ResourceKey(ScopeCredential, "devpass-account"),
		ModelKey:   ResourceKey(ScopeModel, "devpass-account/llmgateway/deepseek-v4-pro"),
	}
}

func classifiedDevPassNotice(observedAt time.Time, text string) *routingerr.Error {
	return routingerr.Classify(routingerr.Input{
		Phase: routingerr.PhasePromptSend, ProviderID: "opencode-acp", Stderr: text, OccurredAt: observedAt,
	})
}

func TestRenewalDueNoticeRetriesWithinTheHourInsteadOfNextMonthlyReset(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	noticeAt := time.Date(2026, 10, 7, 23, 22, 0, 0, kst)
	nextMonthly := time.Date(2026, 11, 7, 23, 20, 0, 0, kst)
	now := noticeAt.Add(-31 * time.Hour)
	engine := NewEngine(
		WithClock(func() time.Time { return now }),
		WithLimitCalendar(fakeLimitCalendar{monthly: nextMonthly}),
	)
	candidate := devPassCandidate()
	profile := Profile{ID: "dynamic", Candidates: []Candidate{candidate}}

	// The account already spent the paid ladder before the renewal day; the
	// strike that follows used to wait for the next monthly reset.
	for range paidUnknownResetLadder {
		engine.RecordResourceFailure(context.Background(), profile, candidate.ID, quotaFailure())
		now = engine.Circuits().Inspect(candidate.BindingKey, now).Until.Add(time.Minute)
	}
	if now.After(noticeAt) {
		t.Fatalf("ladder setup ran past the notice: %s", now)
	}
	now = noticeAt

	engine.RecordResourceFailure(context.Background(), profile, candidate.ID, classifiedDevPassNotice(now, devPassCreditNotice))
	got := engine.Circuits().Inspect(candidate.BindingKey, now)
	if got.Strikes != len(paidUnknownResetLadder)+1 {
		t.Fatalf("strikes = %d, want the strike past the paid ladder", got.Strikes)
	}
	if got.Until.After(now.Add(time.Hour)) {
		t.Fatalf("renewal-day notice blocked until %s, want a retry within the hour (monthly reset %s)", got.Until, nextMonthly)
	}
	if !got.Until.Equal(now.Add(time.Hour)) {
		t.Fatalf("renewal-day notice until = %s, want the last renewal-lag step %s", got.Until, now.Add(time.Hour))
	}
}

func TestRenewalDueNoticeStartsAtTheShortestLagStep(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	engine := NewEngine(
		WithClock(func() time.Time { return now }),
		WithLimitCalendar(fakeLimitCalendar{monthly: now.Add(30 * 24 * time.Hour)}),
	)
	candidate := devPassCandidate()
	profile := Profile{ID: "dynamic", Candidates: []Candidate{candidate}}

	engine.RecordResourceFailure(context.Background(), profile, candidate.ID, classifiedDevPassNotice(now, devPassCreditNotice))
	if got := engine.Circuits().Inspect(candidate.BindingKey, now); !got.Until.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("first renewal-lag strike until = %s, want +30m", got.Until)
	}
}

func TestFutureRenewalDayCapsTheLadder(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(
		WithClock(func() time.Time { return now }),
		WithLimitCalendar(fakeLimitCalendar{monthly: now.Add(30 * 24 * time.Hour)}),
	)
	candidate := devPassCandidate()
	profile := Profile{ID: "dynamic", Candidates: []Candidate{candidate}}
	for range paidUnknownResetLadder {
		engine.RecordResourceFailure(context.Background(), profile, candidate.ID, quotaFailure())
		now = engine.Circuits().Inspect(candidate.BindingKey, now).Until.Add(time.Minute)
	}

	notice := "AI_APICallError: Dev Plan credit limit reached. Upgrade your plan or wait for renewal on 10/20/2026."
	engine.RecordResourceFailure(context.Background(), profile, candidate.ID, classifiedDevPassNotice(now, notice))
	renewalStart := time.Date(2026, 10, 20, 0, 0, 0, 0, time.FixedZone("UTC+14", 14*60*60))
	if got := engine.Circuits().Inspect(candidate.BindingKey, now); !got.Until.Equal(renewalStart) {
		t.Fatalf("future renewal until = %s, want the stated renewal day %s rather than the monthly reset", got.Until, renewalStart)
	}
}
