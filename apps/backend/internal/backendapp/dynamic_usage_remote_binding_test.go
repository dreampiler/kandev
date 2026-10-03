package backendapp

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/registry"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// fakeUsageClient stands in for a provider account. It records whether it was
// consulted at all, which is what proves a remote candidate never reaches the
// host's account reader.
type fakeUsageClient struct {
	usage   *agentusage.ProviderUsage
	queried int
}

func (f *fakeUsageClient) FetchUsage(context.Context) (*agentusage.ProviderUsage, error) {
	f.queried++
	return f.usage, nil
}

// stubProxyResolver supplies the account reader directly. It stands in for the
// per-profile credential binding path, which the adapter consults before its
// host-home fallback.
type stubProxyResolver struct {
	client *fakeUsageClient
}

func (s stubProxyResolver) Resolve(
	*settingsmodels.AgentProfile,
) (agentusage.ProviderUsageClient, string, bool) {
	return s.client, "remote-binding-test", true
}

// TestRemoteExecutionDoesNotBorrowHostAccountUsage pins the half of AC-003.3
// that model-scope filtering cannot cover.
//
// Two candidates run the same agent against the same provider account reader,
// differing only in where they execute. The host one is answered from the host's
// credentials; the container/SSH one must be unknown, because the host file is a
// different account than the one its agent authenticates with. Reporting the
// host's reading for a remote candidate is the substitution the requirement
// forbids, and it is invisible without this test because both candidates look
// identical at the settings layer.
func TestRemoteExecutionDoesNotBorrowHostAccountUsage(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	client := &fakeUsageClient{usage: &agentusage.ProviderUsage{
		Provider: "anthropic",
		Windows: []agentusage.UtilizationWindow{{
			Label: "5-hour", UtilizationPct: 80,
			ResetAt: now.Add(time.Hour), DurationSeconds: 18000,
			StartAt: now.Add(-4 * time.Hour),
		}},
		FetchedAt: now,
	}}
	adapter := &usageProviderAdapter{
		svc: agentusage.NewUsageService(),
		settingsStore: &stubSettingsStore{profiles: map[string]*settingsmodels.AgentProfile{
			"candidate-a": {ID: "candidate-a", AgentID: "antigravity-acp", BillingType: "subscription"},
		}},
		agentRegistry: subscriptionRegistry(t),
		proxyResolver: stubProxyResolver{client: client},
	}
	adapter.svc.Register("candidate-a", client, "test-cache-key")
	snapshot := newDynamicUsageSnapshot(adapter, nil, func() time.Time { return now })

	host := automaticCandidate("candidate-a", dynamicruntime.UsageAutomatic, dynamicruntime.CostSubscription)
	remote := host
	remote.RemoteExecution = true

	hostScores, err := snapshot.UsageSnapshot(context.Background(), dynamicruntime.Profile{
		ID: "p", Version: 1, Candidates: []dynamicruntime.Candidate{host},
	})
	if err != nil {
		t.Fatalf("UsageSnapshot: %v", err)
	}
	if !hostScores["candidate-a"].Known {
		t.Fatalf("host score = %#v, want the host's own account usage", hostScores["candidate-a"])
	}
	if client.queried != 1 {
		t.Fatalf("host client queried %d times, want exactly one", client.queried)
	}

	remoteScores, err := snapshot.UsageSnapshot(context.Background(), dynamicruntime.Profile{
		ID: "p", Version: 1, Candidates: []dynamicruntime.Candidate{remote},
	})
	if err != nil {
		t.Fatalf("UsageSnapshot: %v", err)
	}
	if remoteScores["candidate-a"].Known {
		t.Fatalf("remote score = %#v, want unknown rather than the host account's usage",
			remoteScores["candidate-a"])
	}
	// The strongest evidence: the remote decision never consulted the host reader
	// at all, so the two execution environments cannot be sharing a reading.
	if client.queried != 1 {
		t.Fatalf("host client queried %d times, want it untouched by the remote candidate", client.queried)
	}
}

// TestManualWindowsStillWorkForRemoteCandidates pins that only the automatic path
// is restricted. A manual window is summed from the task ledger by concrete
// candidate, so it is the candidate's own recorded usage regardless of where it
// runs, and refusing it would lose real evidence.
func TestManualWindowsStillWorkForRemoteCandidates(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	adapter := &usageProviderAdapter{
		svc: agentusage.NewUsageService(),
		settingsStore: &stubSettingsStore{profiles: map[string]*settingsmodels.AgentProfile{
			"candidate-a": {ID: "candidate-a", AgentID: "antigravity-acp", BillingType: "subscription"},
		}},
		agentRegistry: subscriptionRegistry(t),
	}
	manual := &fakeManualTotals{totals: sqliterepo.ManualWindowUsage{
		CostSubcents: 500_000, EventCount: 2,
	}}
	snapshot := newDynamicUsageSnapshot(adapter, manual, func() time.Time { return now })

	candidate := automaticCandidate("candidate-a", dynamicruntime.UsageManual, dynamicruntime.CostSubscription)
	candidate.RemoteExecution = true
	candidate.Selection.Model.Windows = []dynamicruntime.UsageWindow{{
		Period: agentusage.WindowPeriodMonth, Unit: agentusage.WindowUnitMoney,
		Limit: "100.00", ResetAnchor: "00:00 day 1", Timezone: "UTC",
	}}
	scores, err := snapshot.UsageSnapshot(context.Background(), dynamicruntime.Profile{
		ID: "p", Version: 1, Candidates: []dynamicruntime.Candidate{candidate},
	})
	if err != nil {
		t.Fatalf("UsageSnapshot: %v", err)
	}
	if !scores["candidate-a"].Known {
		t.Fatalf("manual score = %#v, want the recorded ledger usage for a remote candidate",
			scores["candidate-a"])
	}
	if manual.calls != 1 {
		t.Fatalf("ledger reads = %d, want exactly one", manual.calls)
	}
}

type stubSettingsStore struct {
	settingsstore.Repository
	profiles map[string]*settingsmodels.AgentProfile
}

func (s *stubSettingsStore) GetAgentProfile(
	_ context.Context, profileID string,
) (*settingsmodels.AgentProfile, error) {
	if profile, ok := s.profiles[profileID]; ok {
		return profile, nil
	}
	return nil, nil
}

// subscriptionRegistry provides a real subscription-billing agent whose usage
// client is registered per profile, so the test controls what the account reader
// returns instead of depending on host credential files.
func subscriptionRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.NewRegistry(newTestLogger())
	if err := reg.Register(agents.NewAntigravityACP()); err != nil {
		t.Fatalf("register subscription agent: %v", err)
	}
	return reg
}
