package backendapp

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

type reservationUsageClient struct {
	usage   *agentusage.ProviderUsage
	err     error
	onFetch func()
}

func (c *reservationUsageClient) FetchUsage(context.Context) (*agentusage.ProviderUsage, error) {
	if c.onFetch != nil {
		c.onFetch()
	}
	if c.err != nil {
		return nil, c.err
	}
	return c.usage, nil
}

// @covers AC-AGENTS-TIER-SELECTION-003.7
func TestDynamicUsageReservationFreshFetchUsesOnePostLookupClock(t *testing.T) {
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	opusAt := now.Add(time.Second)
	solAt := now.Add(2 * time.Second)
	clients := reservationUsageResolver{
		"opus": &reservationUsageClient{usage: weeklyUsage(opusAt, 33), onFetch: func() { now = opusAt }},
		"sol":  &reservationUsageClient{usage: weeklyUsage(solAt, 4), onFetch: func() { now = solAt }},
	}
	snapshot, profile := reservationUsageFixture(func() time.Time { return now }, clients)
	scores, err := snapshot.UsageSnapshot(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for id, observedAt := range map[string]time.Time{"opus": opusAt, "sol": solAt} {
		score := scores[id]
		if !score.Known || !score.ObservedAt.Equal(observedAt) {
			t.Fatalf("%s = %#v, want the fresh original observation", id, score)
		}
		wantElapsed := solAt.Sub(observedAt.Add(-24*time.Hour)).Seconds() / (7 * 24 * 3600)
		if score.ElapsedFraction != wantElapsed {
			t.Fatalf("%s elapsed = %v, want %v at the shared post-lookup clock", id, score.ElapsedFraction, wantElapsed)
		}
	}
	checkUsageReservationChoice(t, snapshot, profile, "sol")
}

type reservationUsageResolver map[string]agentusage.ProviderUsageClient

func (r reservationUsageResolver) Resolve(
	profile *settingsmodels.AgentProfile, _ string,
) (agentusage.ProviderUsageClient, string, bool) {
	client, ok := r[profile.ID]
	return client, "reservation-" + profile.ID, ok
}

func reservationUsageFixture(
	now func() time.Time, clients reservationUsageResolver,
) (*dynamicUsageSnapshot, dynamicruntime.Profile) {
	store := &agentRowSettingsStore{profiles: map[string]*settingsmodels.AgentProfile{
		"opus": {ID: "opus", AgentID: claudeACPAgentID, Model: "opus"},
		"sol":  {ID: "sol", AgentID: codexACPAgentID, Model: "sol"},
	}}
	adapter := &usageProviderAdapter{
		svc: agentusage.NewUsageService(), settingsStore: store, proxyResolver: clients,
	}
	profile := dynamicruntime.Profile{ID: "reserved-tier", Version: 1}
	for i, id := range []string{"opus", "sol", "kimi"} {
		candidate := automaticCandidate(id, dynamicruntime.UsageAutomatic, dynamicruntime.CostSubscription)
		candidate.BindingKey = "binding:" + id
		candidate.Selection.Model.ReservedUserSharePct = 2
		candidate.Selection.JoinPrevious = i == 1
		if i != 1 {
			candidate.Selection.Tier = &dynamicruntime.TierPolicy{
				Mode: dynamicruntime.TierModePace, OnFailure: dynamicruntime.FailureSameTierNext,
			}
		}
		if i == 2 {
			candidate.Selection.Model = dynamicruntime.ModelOptions{
				Cost: dynamicruntime.CostFree, UsageSource: dynamicruntime.UsageNone,
			}
		}
		profile.Candidates = append(profile.Candidates, candidate)
	}
	return newDynamicUsageSnapshot(adapter, nil, now), profile
}

func checkUsageReservationChoice(t *testing.T, snapshot *dynamicUsageSnapshot, profile dynamicruntime.Profile, want string) {
	t.Helper()
	pick := func(int) int { return 0 }
	engine := dynamicruntime.NewEngine(dynamicruntime.WithClock(snapshot.now),
		dynamicruntime.WithRandomPick(pick), dynamicruntime.WithUsageSnapshotProvider(snapshot))
	decision, err := engine.SelectContext(context.Background(), "session", profile, 0, "")
	if err != nil || decision.ExecutionProfileID != want {
		t.Fatalf("SelectContext = %q, %v; want %q", decision.ExecutionProfileID, err, want)
	}
	scores, err := snapshot.UsageSnapshot(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	preview := dynamicruntime.PreviewSelectionWith(profile, dynamicruntime.RankOptions{Scores: scores, Pick: pick},
		nil, "", dynamicruntime.SelectionChain{}, snapshot.now())
	if preview.CandidateID != decision.ExecutionProfileID {
		t.Fatalf("preview = %q, live = %q; want agreement", preview.CandidateID, decision.ExecutionProfileID)
	}
}

// @covers AC-AGENTS-TIER-SELECTION-003.7
func TestDynamicUsageReservationObservationValidity(t *testing.T) {
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		fetched time.Time
		change  func(*agentusage.ProviderUsage)
		known   bool
	}{
		{name: "current observation", fetched: now, known: true},
		{name: "just under thirty minutes", fetched: now.Add(-30*time.Minute + time.Nanosecond), known: true},
		{name: "exactly thirty minutes", fetched: now.Add(-30 * time.Minute)},
		{name: "older observation", fetched: now.Add(-31 * time.Minute)},
		{name: "missing timestamp"},
		{name: "future timestamp", fetched: now.Add(time.Nanosecond)},
		{name: "expired quota window", fetched: now, change: func(u *agentusage.ProviderUsage) {
			u.Windows[0].ResetAt = now
		}},
		{name: "different model window", fetched: now, change: func(u *agentusage.ProviderUsage) {
			u.Windows[0].ModelID = "other-model"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clients := make(reservationUsageResolver)
			for id, pct := range map[string]float64{"opus": 33, "sol": 4} {
				usage := weeklyUsage(now, pct)
				usage.FetchedAt = tt.fetched
				usage.Stale = !tt.fetched.Equal(now)
				if tt.change != nil {
					tt.change(usage)
				}
				clients[id] = &reservationUsageClient{usage: usage}
			}
			snapshot, profile := reservationUsageFixture(func() time.Time { return now }, clients)
			scores, err := snapshot.UsageSnapshot(context.Background(), profile)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"opus", "sol"} {
				if score := scores[id]; score.Known != tt.known || score.HasRecord != tt.known {
					t.Fatalf("%s evidence = %#v; want known/recorded %v", id, score, tt.known)
				}
			}
			want := "opus"
			if tt.known {
				want = "sol"
			}
			checkUsageReservationChoice(t, snapshot, profile, want)
		})
	}
}

// @covers AC-AGENTS-TIER-SELECTION-003.5, AC-AGENTS-TIER-SELECTION-003.7
func TestDynamicUsageReservationRestartWithoutObservation(t *testing.T) {
	now := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	clients := make(reservationUsageResolver)
	for _, id := range []string{"opus", "sol"} {
		clients[id] = &reservationUsageClient{err: &agentusage.FetchError{
			Provider: id, Reason: agentusage.FailureHTTPStatus, Status: http.StatusTooManyRequests,
		}}
	}
	snapshot, profile := reservationUsageFixture(func() time.Time { return now }, clients)
	scores, err := snapshot.UsageSnapshot(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opus", "sol"} {
		if scores[id].Known || scores[id].HasRecord {
			t.Fatalf("%s = %#v, want no observed usage", id, scores[id])
		}
	}
	checkUsageReservationChoice(t, snapshot, profile, "opus")
}

// @covers AC-COSTS-PROVIDER-USAGE-001.5, AC-AGENTS-TIER-SELECTION-003.7
func TestDynamicUsageReservationRecentCachedSuccessAfter429(t *testing.T) {
	for _, tt := range []struct {
		name string
		opus float64
		sol  float64
		want string
	}{
		{name: "capacity remains in first tier", opus: 33, sol: 4, want: "sol"},
		{name: "recent reserved threshold still blocks", opus: 100, sol: 98, want: "kimi"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				observedAt := time.Now()
				clients := reservationUsageResolver{
					"opus": &reservationUsageClient{usage: weeklyUsage(observedAt, tt.opus)},
					"sol":  &reservationUsageClient{usage: weeklyUsage(observedAt, tt.sol)},
				}
				snapshot, profile := reservationUsageFixture(time.Now, clients)
				if _, err := snapshot.UsageSnapshot(context.Background(), profile); err != nil {
					t.Fatal(err)
				}
				for _, client := range clients {
					client.(*reservationUsageClient).err = &agentusage.FetchError{
						Reason: agentusage.FailureHTTPStatus, Status: http.StatusTooManyRequests,
					}
				}
				// Advance the virtual clock beyond the five-minute fresh cache.
				time.Sleep(6 * time.Minute)
				checkUsageReservationChoice(t, snapshot, profile, tt.want)
				for _, id := range []string{"opus", "sol"} {
					reading := snapshot.usage.ProfileUsage(context.Background(), id)
					if reading.State != profileUsageOK || reading.Usage == nil || !reading.Usage.Stale ||
						!reading.Usage.FetchedAt.Equal(observedAt) {
						t.Fatalf("%s = %#v, want the original stale observation after 429", id, reading)
					}
				}
			})
		})
	}
}
