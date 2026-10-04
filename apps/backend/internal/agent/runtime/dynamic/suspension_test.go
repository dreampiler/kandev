package dynamic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

type fakeLimitCalendar struct {
	exhausted time.Time
	monthly   time.Time
}

func (c fakeLimitCalendar) ExhaustedUntil(context.Context, Candidate, time.Time) (time.Time, bool) {
	return c.exhausted, !c.exhausted.IsZero()
}

func (c fakeLimitCalendar) MonthlyReset(context.Context, Candidate, time.Time) (time.Time, bool) {
	return c.monthly, !c.monthly.IsZero()
}

func goCandidate(id, model string) Candidate {
	return Candidate{
		ID: id, Enabled: true, ModelID: model,
		BindingKey: ResourceKey(ScopeCredential, "go-account"),
		ModelKey:   ResourceKey(ScopeModel, "go-account/"+model),
	}
}

func quotaFailure() *routingerr.Error {
	return &routingerr.Error{Code: routingerr.CodeQuotaLimited}
}

func TestModelScopedLimitLeavesSiblingModelsSelectable(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	glm := goCandidate("glm", "opencode-go/glm-5")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi, glm}}

	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())

	if got := engine.SuspensionFor(kimi, now); got.State != ResourceWaiting || got.Scope != SuspensionScopeModel {
		t.Fatalf("kimi suspension = %#v, want waiting model scope", got)
	}
	if got := engine.SuspensionFor(glm, now); got.State != ResourceAvailable {
		t.Fatalf("glm suspension = %#v, want available", got)
	}
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil || decision.ExecutionProfileID != "glm" {
		t.Fatalf("Select = %#v, %v; want glm", decision, err)
	}
}

func TestAccountScopedFailuresStillPauseTheCredential(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	glm := goCandidate("glm", "opencode-go/glm-5")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi, glm}}

	engine.RecordResourceFailure(context.Background(), profile, "kimi",
		&routingerr.Error{Code: routingerr.CodeAuthRequired})

	if got := engine.SuspensionFor(glm, now); got.State != ResourceWaiting || got.Scope != SuspensionScopeCredential {
		t.Fatalf("glm suspension = %#v, want waiting credential scope", got)
	}
}

func TestOpenCodeGoLadderEscalatesToMonthlyReset(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	monthly := now.Add(20 * 24 * time.Hour)
	engine := NewEngine(
		WithClock(func() time.Time { return now }),
		WithLimitCalendar(fakeLimitCalendar{monthly: monthly}),
	)
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi}}

	steps := []time.Duration{2 * time.Hour, 2 * time.Hour, 2 * time.Hour, 24 * time.Hour}
	for index, step := range steps {
		engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())
		got := engine.Circuits().Inspect(kimi.ModelKey, now)
		if !got.Until.Equal(now.Add(step)) || got.Strikes != index+1 {
			t.Fatalf("strike %d: %#v, want until +%s", index+1, got, step)
		}
		now = got.Until.Add(time.Minute)
	}
	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())
	if got := engine.Circuits().Inspect(kimi.ModelKey, now); !got.Until.Equal(monthly) {
		t.Fatalf("fifth strike until = %s, want monthly reset %s", got.Until, monthly)
	}
}

func TestRepeatFailureInRunningBlockCountsOnce(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi}}

	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())
	first := engine.Circuits().Inspect(kimi.ModelKey, now)
	now = now.Add(10 * time.Minute)
	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())
	second := engine.Circuits().Inspect(kimi.ModelKey, now)
	if second.Strikes != 1 || second.Until.Before(first.Until) {
		t.Fatalf("repeat failure = %#v, want strike 1 and no earlier than %s", second, first.Until)
	}
}

func TestMonthlyResetCapsLadderAndKnownResetWins(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	monthly := now.Add(30 * time.Minute)
	engine := NewEngine(
		WithClock(func() time.Time { return now }),
		WithLimitCalendar(fakeLimitCalendar{monthly: monthly}),
	)
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi}}
	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())
	if got := engine.Circuits().Inspect(kimi.ModelKey, now); !got.Until.Equal(monthly) {
		t.Fatalf("capped until = %s, want %s", got.Until, monthly)
	}

	hinted := NewEngine(
		WithClock(func() time.Time { return now }),
		WithLimitCalendar(fakeLimitCalendar{exhausted: now.Add(40 * time.Hour)}),
	)
	reset := now.Add(3 * time.Hour)
	hinted.RecordResourceFailure(context.Background(), profile, "kimi",
		&routingerr.Error{Code: routingerr.CodeQuotaLimited, ResetHint: &reset})
	if got := hinted.Circuits().Inspect(kimi.ModelKey, now); !got.Until.Equal(reset) {
		t.Fatalf("hinted until = %s, want reset hint %s", got.Until, reset)
	}

	observed := NewEngine(
		WithClock(func() time.Time { return now }),
		WithLimitCalendar(fakeLimitCalendar{exhausted: now.Add(40 * time.Hour)}),
	)
	observed.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())
	if got := observed.Circuits().Inspect(kimi.ModelKey, now); !got.Until.Equal(now.Add(40 * time.Hour)) {
		t.Fatalf("observed until = %s, want exhausted window reset", got.Until)
	}
}

func TestOnlyRealOutputClearsStrikes(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi}}

	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())
	now = now.Add(3 * time.Hour)
	decision, err := engine.Select("session", profile, 0, "")
	if err != nil {
		t.Fatalf("probe select: %v", err)
	}
	engine.ReleaseProbe(decision, true)
	if got := engine.Circuits().Inspect(kimi.ModelKey, now); got.State != ResourceAvailable || got.Strikes != 1 {
		t.Fatalf("after launch success = %#v, want closed with strike kept", got)
	}
	if next := engine.Circuits().NextStrike(kimi.ModelKey); next != 2 {
		t.Fatalf("next strike = %d, want 2", next)
	}
	engine.RecordResourceSuccess(kimi)
	if next := engine.Circuits().NextStrike(kimi.ModelKey); next != 1 {
		t.Fatalf("next strike after output = %d, want 1", next)
	}
}

func TestInspectSeparatesExpiredFromWaiting(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi}}
	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())

	if got := engine.SuspensionFor(kimi, now); got.State != ResourceWaiting || !got.Blocked() {
		t.Fatalf("running = %#v, want blocked waiting", got)
	}
	later := now.Add(3 * time.Hour)
	if got := engine.SuspensionFor(kimi, later); got.State != ResourceExpired || got.Blocked() {
		t.Fatalf("expired = %#v, want unblocked expired", got)
	}
	if !engine.Circuits().IsOpen(kimi.ModelKey, later) {
		t.Fatal("expired circuit must stay open until a probe is claimed")
	}
}

func TestManualBlockSuspendsCandidate(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	blocked := goCandidate("kimi", "opencode-go/kimi-k2")
	blocked.SuspendedUntil = now.Add(time.Hour)
	profile := Profile{ID: "dynamic", Candidates: []Candidate{blocked}}

	got := engine.SuspensionFor(blocked, now)
	if got.State != ResourceWaiting || got.Source != SuspensionSourceManual || got.Scope != SuspensionScopeProvider {
		t.Fatalf("manual suspension = %#v", got)
	}
	var noCandidate *NoEligibleCandidateError
	if _, err := engine.Select("session", profile, 0, ""); !errors.As(err, &noCandidate) {
		t.Fatalf("Select error = %v, want no eligible candidate", err)
	}
}

func TestExhaustedSelectionWaitsForEarliestSuspensionEnd(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	engine := NewEngine(WithClock(func() time.Time { return now }))
	kimi := goCandidate("kimi", "opencode-go/kimi-k2")
	glm := goCandidate("glm", "opencode-go/glm-5")
	glm.SuspendedUntil = now.Add(time.Hour)
	profile := Profile{ID: "dynamic", Candidates: []Candidate{kimi, glm}}
	engine.RecordResourceFailure(context.Background(), profile, "kimi", quotaFailure())

	var observedSession string
	var observedDeadline time.Time
	engine.SetResourceWaitObserver(func(sessionID string, _ int64, deadline time.Time) {
		observedSession, observedDeadline = sessionID, deadline
	})
	if _, err := engine.Select("session", profile, 0, ""); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select error = %v, want no eligible candidate", err)
	}
	if observedSession != "session" || !observedDeadline.Equal(now.Add(time.Hour)) {
		t.Fatalf("observer = %q %s, want session at +1h", observedSession, observedDeadline)
	}
	state, _, err := engine.LoadState(context.Background(), "session")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if deadline := policyDeadlineForTest(t, state.PolicyStateJSON); !deadline.Equal(now.Add(time.Hour)) {
		t.Fatalf("persisted deadline = %s", deadline)
	}
}

func TestExhaustedSelectionWithoutSuspensionStaysManual(t *testing.T) {
	engine := NewEngine()
	profile := Profile{ID: "dynamic", Candidates: []Candidate{{ID: "only", Enabled: true}}}
	called := false
	engine.SetResourceWaitObserver(func(string, int64, time.Time) { called = true })
	if _, err := engine.Select("session", profile, 0, "only"); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select error = %v, want no eligible candidate", err)
	}
	if called {
		t.Fatal("a wait not caused by suspensions must not schedule a retry")
	}
}

func TestSuspensionPolicyClassifiesModels(t *testing.T) {
	cases := []struct {
		model       string
		provider    string
		free        bool
		modelScoped bool
	}{
		{"opencode-go/kimi-k2", "opencode-go", false, true},
		{"opencode-go/minimax-m2.5-free", "opencode-go", true, true},
		{"openrouter/qwen/qwen3-coder:free", "openrouter", true, true},
		{"llmgateway/claude-sonnet", "llmgateway", false, false},
		{"sonnet", "", false, false},
	}
	for _, tc := range cases {
		if got := ProviderOf(tc.model); got != tc.provider {
			t.Errorf("ProviderOf(%q) = %q, want %q", tc.model, got, tc.provider)
		}
		if got := IsFreeModel(tc.model); got != tc.free {
			t.Errorf("IsFreeModel(%q) = %v, want %v", tc.model, got, tc.free)
		}
		if got := ModelScoped(tc.model); got != tc.modelScoped {
			t.Errorf("ModelScoped(%q) = %v, want %v", tc.model, got, tc.modelScoped)
		}
	}
}

func TestProviderLimitNextMonthlyResetRepeatsAndClamps(t *testing.T) {
	seoul, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	anchor := time.Date(2026, 1, 31, 14, 0, 0, 0, seoul)
	limit := ProviderLimit{MonthlyResetAt: &anchor, MonthlyResetTimezone: "Asia/Seoul"}
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 2, 10, 0, 0, 0, 0, seoul), time.Date(2026, 2, 28, 14, 0, 0, 0, seoul)},
		{time.Date(2026, 10, 31, 14, 0, 0, 0, seoul), time.Date(2026, 11, 30, 14, 0, 0, 0, seoul)},
		{time.Date(2026, 10, 31, 13, 59, 0, 0, seoul), time.Date(2026, 10, 31, 14, 0, 0, 0, seoul)},
	}
	for _, tc := range cases {
		got, ok := limit.NextMonthlyReset(tc.now)
		if !ok || !got.Equal(tc.want) {
			t.Errorf("NextMonthlyReset(%s) = %s, %v; want %s", tc.now, got, ok, tc.want)
		}
	}
	if _, ok := (ProviderLimit{}).NextMonthlyReset(time.Now()); ok {
		t.Error("a provider without a monthly reset must not report one")
	}
}

func TestProviderLimitBlockSkipsFreeModels(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	block := now.Add(time.Hour)
	limit := ProviderLimit{BlockUntil: &block}
	if until, ok := limit.BlockedUntil("opencode-go/kimi-k2", now); !ok || !until.Equal(block) {
		t.Fatalf("paid model block = %s, %v", until, ok)
	}
	if _, ok := limit.BlockedUntil("opencode-go/minimax-m2.5-free", now); ok {
		t.Fatal("a free model must not share the provider block")
	}
	if _, ok := limit.BlockedUntil("opencode-go/kimi-k2", block.Add(time.Second)); ok {
		t.Fatal("an expired block must not apply")
	}
}

func policyDeadlineForTest(t *testing.T, raw string) time.Time {
	t.Helper()
	var state PolicyState
	if err := json.Unmarshal([]byte(raw), &state); err != nil || state.Deadline == nil || !state.ResourceWait {
		t.Fatalf("policy state %q has no resource wait deadline: %v", raw, err)
	}
	return *state.Deadline
}
