package dynamic

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// failoverCandidate builds a candidate the way the runtime resolver does for a
// profile without its own credential: a profile-scoped binding, the model key
// for model-metered providers, and the agent+provider account.
func failoverCandidate(id, model string) Candidate {
	binding := ResourceKey(ScopeCredential, "profile:"+id)
	candidate := Candidate{
		ID: id, Enabled: true, ModelID: model, BindingKey: binding,
		AccountKey: ResourceKey(ScopeCredential, "account:opencode-acp|"+ProviderOf(model)),
	}
	if ModelScopedCandidate(candidate) {
		candidate.ModelKey = ResourceKey(ScopeModel, binding+"|"+model)
	}
	return candidate
}

type failoverClock struct{ now time.Time }

func (c *failoverClock) Now() time.Time { return c.now }

func newFailoverEngine(start time.Time) (*Engine, *failoverClock) {
	clock := &failoverClock{now: start}
	return NewEngine(WithClock(clock.Now)), clock
}

func failureOf(code routingerr.Code, rule string) *routingerr.Error {
	return &routingerr.Error{Code: code, ClassifierRule: rule, FallbackAllowed: true, AutoRetryable: true}
}

// recordAfterExpiry records a failure once the previous block has ended, the
// way a fresh probe would meet it, and returns the new block length.
func recordAfterExpiry(t *testing.T, engine *Engine, clock *failoverClock, profile Profile, candidate Candidate, key string, failure *routingerr.Error) time.Duration {
	t.Helper()
	if status := engine.Circuits().Inspect(key, clock.now); status.State == ResourceWaiting {
		clock.now = status.Until.Add(time.Second)
	}
	engine.RecordResourceFailure(context.Background(), profile, candidate.ID, failure)
	status := engine.Circuits().Inspect(key, clock.now)
	if status.State != ResourceWaiting {
		t.Fatalf("%s after failure = %#v, want a running block", key, status)
	}
	return status.Until.Sub(clock.now)
}

func TestEndpointUnavailableClimbsItsLadderOnItsPathOnly(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	ocg := failoverCandidate("ocg-longcat", "opencode-go/longcat-2.5-preview-free")
	ocz := failoverCandidate("ocz-longcat", "opencode/longcat-2.5-preview-free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{ocg, ocz}}
	failure := failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleOpenCodeEndpointUnavailable)

	want := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for strike, length := range want {
		got := recordAfterExpiry(t, engine, clock, profile, ocg, ocg.pathModelKey(), failure)
		if got != length {
			t.Fatalf("strike %d block = %v, want %v", strike+1, got, length)
		}
		if engine.SuspensionFor(ocz, clock.now).Blocked() {
			t.Fatalf("strike %d: the same model on Zen was blocked by a Go endpoint failure", strike+1)
		}
	}
}

func TestUnrecognizedUnavailableClimbsTheTransientLadder(t *testing.T) {
	start := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	candidate := failoverCandidate("paid", "llmgateway/glm-5.3-flash")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{candidate}}
	for _, code := range []routingerr.Code{routingerr.CodeProviderUnavailable, routingerr.CodeProviderOverloaded, routingerr.CodeModelCapacity} {
		engine, clock := newFailoverEngine(start)
		failure := failureOf(code, "provider.unavailable.v1")
		for strike, length := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, time.Hour} {
			if got := recordAfterExpiry(t, engine, clock, profile, candidate, candidate.pathModelKey(), failure); got != length {
				t.Fatalf("%s strike %d block = %v, want %v", code, strike+1, got, length)
			}
		}
	}
}

func TestCleanTurnRestartsTheLadder(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	candidate := failoverCandidate("ocg-longcat", "opencode-go/longcat-2.5-preview-free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{candidate}}
	failure := failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleOpenCodeEndpointUnavailable)
	for range 3 {
		recordAfterExpiry(t, engine, clock, profile, candidate, candidate.pathModelKey(), failure)
	}
	clock.now = clock.now.Add(time.Hour)
	engine.RecordResourceSuccess(candidate)
	if got := recordAfterExpiry(t, engine, clock, profile, candidate, candidate.pathModelKey(), failure); got != time.Minute {
		t.Fatalf("block after a clean turn = %v, want the first step", got)
	}
}

func TestProviderResetHintOutranksTheLadder(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	candidate := failoverCandidate("ocg-longcat", "opencode-go/longcat-2.5-preview-free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{candidate}}
	hint := clock.now.Add(40 * time.Minute)
	failure := failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleOpenCodeEndpointUnavailable)
	failure.ResetHint = &hint
	if got := recordAfterExpiry(t, engine, clock, profile, candidate, candidate.pathModelKey(), failure); got != 40*time.Minute {
		t.Fatalf("block = %v, want the provider's 40 minutes", got)
	}
}

func TestModelHostFailureOnTwoPathsBlocksTheModelEverywhere(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	ocz := failoverCandidate("ocz-bunny", "opencode/space-bunny-free")
	ocg := failoverCandidate("ocg-bunny", "opencode-go/space-bunny-free")
	or := failoverCandidate("or-bunny", "openrouter/stealth/space-bunny:free")
	other := failoverCandidate("ocz-pickle", "opencode/big-pickle")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{ocz, ocg, or, other}}
	failure := failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleProviderHeaderTimeout)

	if got := recordAfterExpiry(t, engine, clock, profile, ocz, ocz.pathModelKey(), failure); got != 5*time.Minute {
		t.Fatalf("first model-host block = %v, want 5m", got)
	}
	if engine.SuspensionFor(ocg, clock.now).Blocked() || engine.SuspensionFor(or, clock.now).Blocked() {
		t.Fatal("one path's failure blocked the model on its other paths")
	}
	clock.now = clock.now.Add(4 * time.Minute)
	engine.RecordResourceFailure(context.Background(), profile, ocg.ID, failure)
	for _, candidate := range []Candidate{ocz, ocg, or} {
		suspension := engine.SuspensionFor(candidate, clock.now)
		if !suspension.Blocked() || suspension.Scope != SuspensionScopeModel {
			t.Fatalf("%s after a second path failed = %#v, want the model blocked everywhere", candidate.ID, suspension)
		}
	}
	if engine.SuspensionFor(other, clock.now).Blocked() {
		t.Fatal("a model-host failure blocked a different model")
	}
}

func TestModelHostFailuresOutsideTheWindowStayOnTheirPath(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	ocz := failoverCandidate("ocz-bunny", "opencode/space-bunny-free")
	ocg := failoverCandidate("ocg-bunny", "opencode-go/space-bunny-free")
	or := failoverCandidate("or-bunny", "openrouter/stealth/space-bunny:free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{ocz, ocg, or}}
	failure := failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleProviderStreamServerError)
	engine.RecordResourceFailure(context.Background(), profile, ocz.ID, failure)
	clock.now = clock.now.Add(modelHostWindow + time.Minute)
	engine.RecordResourceFailure(context.Background(), profile, ocg.ID, failure)
	if engine.SuspensionFor(or, clock.now).Blocked() {
		t.Fatal("failures more than the window apart blocked the model everywhere")
	}
}

func TestZenDailyLimitWaitsForTheNextUTCDay(t *testing.T) {
	start := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	engine, clock := newFailoverEngine(start)
	ocz := failoverCandidate("ocz-bunny", "opencode/space-bunny-free")
	ocg := failoverCandidate("ocg-longcat", "opencode-go/longcat-2.5-preview-free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{ocz, ocg}}
	failure := failureOf(routingerr.CodeRateLimited, routingerr.RuleOpenCodeRateTryLater)

	got := recordAfterExpiry(t, engine, clock, profile, ocz, ocz.pathModelKey(), failure)
	if want := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC).Sub(start); got != want {
		t.Fatalf("Zen daily limit block = %v, want until 00:00 UTC (%v)", got, want)
	}
	if got := recordAfterExpiry(t, engine, clock, profile, ocg, ocg.ModelKey, failure); got != time.Minute {
		t.Fatalf("the same notice on Go = %v, want the free ladder's first step", got)
	}
}

func TestOpenRouterFreeAllowanceBlocksTheFreeTierOfTheAccount(t *testing.T) {
	start := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	engine, clock := newFailoverEngine(start)
	nemotron := failoverCandidate("or-nemotron", "openrouter/nvidia/nemotron-3-ultra-550b-a55b:free")
	apodex := failoverCandidate("or-apodex", "openrouter/apodex/apodex-1.1-mini:free")
	paid := failoverCandidate("or-paid", "openrouter/anthropic/claude-sonnet")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{nemotron, apodex, paid}}
	engine.RecordResourceFailure(context.Background(), profile, nemotron.ID,
		failureOf(routingerr.CodeRateLimited, routingerr.RuleOpenCodeFreeModelsPerDay))
	if !engine.SuspensionFor(apodex, clock.now).Blocked() {
		t.Fatal("the account's other free model stayed selectable after the shared daily allowance ran out")
	}
	if engine.SuspensionFor(paid, clock.now).Blocked() {
		t.Fatal("the shared free allowance blocked a paid model")
	}
}

func TestGoUsageLimitBlocksPaidGoModelsOnly(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	glm := failoverCandidate("ocg-glm", "opencode-go/glm-5.3-flash")
	deepseek := failoverCandidate("ocg-deepseek", "opencode-go/deepseek-v4.1-flash")
	longcat := failoverCandidate("ocg-longcat", "opencode-go/longcat-2.5-preview-free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{glm, deepseek, longcat}}
	failure := failureOf(routingerr.CodeQuotaLimited, routingerr.RuleOpenCodeGoUsageLimit)

	if got := recordAfterExpiry(t, engine, clock, profile, glm, glm.accountTierKey(), failure); got != 5*time.Hour {
		t.Fatalf("first Go limit block = %v, want 5h", got)
	}
	if !engine.SuspensionFor(deepseek, clock.now).Blocked() {
		t.Fatal("another paid Go model stayed selectable under the shared Go limit")
	}
	if engine.SuspensionFor(longcat, clock.now).Blocked() {
		t.Fatal("the Go limit blocked a free Go model")
	}
	if got := recordAfterExpiry(t, engine, clock, profile, glm, glm.accountTierKey(), failure); got != 24*time.Hour {
		t.Fatalf("second Go limit block = %v, want 24h", got)
	}
}

func TestCreditLimitBlocksTheAccountUntilItsRenewal(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 9, 30, 3, 0, 0, 0, time.UTC))
	glm := failoverCandidate("dp-glm", "llmgateway/glm-5.3-flash")
	deepseek := failoverCandidate("dp-deepseek", "llmgateway/deepseek-v4.1-flash")
	zen := failoverCandidate("ocz-pickle", "opencode/big-pickle")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{glm, deepseek, zen}}
	renewal := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	failure := failureOf(routingerr.CodeQuotaLimited, routingerr.RuleOpenCodeCredit)
	failure.RenewalAt = &renewal
	engine.RecordResourceFailure(context.Background(), profile, glm.ID, failure)
	suspension := engine.SuspensionFor(deepseek, clock.now)
	if !suspension.Blocked() || !suspension.Until.Equal(renewal) {
		t.Fatalf("other DevPass model = %#v, want blocked until the renewal %v", suspension, renewal)
	}
	if engine.SuspensionFor(zen, clock.now).Blocked() {
		t.Fatal("a credit limit blocked another account")
	}
}

func TestAccountRateLimitBlocksOnlyItsOwnPath(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	ocz := failoverCandidate("ocz-longcat", "opencode/longcat-2.5-preview-free")
	ocg := failoverCandidate("ocg-longcat", "opencode-go/longcat-2.5-preview-free")
	sibling := failoverCandidate("ocz-pickle", "opencode/big-pickle-free")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{ocz, ocg, sibling}}
	engine.RecordResourceFailure(context.Background(), profile, ocz.ID,
		failureOf(routingerr.CodeRateLimited, "opencode.stderr.rate.v1"))
	if !engine.SuspensionFor(ocz, clock.now).Blocked() {
		t.Fatal("the limited path stayed selectable")
	}
	if engine.SuspensionFor(ocg, clock.now).Blocked() || engine.SuspensionFor(sibling, clock.now).Blocked() {
		t.Fatal("a per-model rate limit blocked another path or another model")
	}
}

func TestWithdrawnModelWaitsADayAndLocalFailureBlocksNothing(t *testing.T) {
	engine, clock := newFailoverEngine(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	ling := failoverCandidate("ocz-ling", "opencode/ling-3.0-flash-fin-free")
	local := failoverCandidate("dp-glm", "llmgateway/glm-5.3-flash")
	profile := Profile{ID: "dynamic", Candidates: []Candidate{ling, local}}
	if got := recordAfterExpiry(t, engine, clock, profile, ling, ling.pathModelKey(),
		failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleOpenCodeModelWithdrawn)); got != 24*time.Hour {
		t.Fatalf("withdrawn model block = %v, want 24h", got)
	}
	engine.RecordResourceFailure(context.Background(), profile, local.ID,
		failureOf(routingerr.CodeProviderUnavailable, routingerr.RuleOpenCodeServiceFailure))
	if engine.SuspensionFor(local, clock.now).Blocked() {
		t.Fatal("a local OpenCode service failure suspended a provider")
	}
}

func TestEveryCatalogueRuleHasAPolicy(t *testing.T) {
	for _, row := range failureCauseRules {
		if _, ok := failureCausePolicies[row.cause]; !ok {
			t.Fatalf("cause %q has no policy", row.cause)
		}
	}
}

func TestModelIdentityFoldsGatewayNames(t *testing.T) {
	cases := map[string]string{
		"opencode-go/longcat-2.5-preview-free":              "longcat-2.5-preview",
		"opencode/longcat-2.5-preview-free":                 "longcat-2.5-preview",
		"openrouter/nvidia/nemotron-3-ultra-550b-a55b:free": "nemotron-3-ultra",
		"opencode/nemotron-3-ultra-free":                    "nemotron-3-ultra",
		"sonnet":                                            "sonnet",
		"default":                                           "",
		"opencode/":                                         "",
	}
	for model, want := range cases {
		if got := ModelIdentity(model); got != want {
			t.Fatalf("ModelIdentity(%q) = %q, want %q", model, got, want)
		}
	}
}
