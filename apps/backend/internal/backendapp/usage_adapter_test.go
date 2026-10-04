package backendapp

import (
	"context"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

// agentRowSettingsStore stores profiles the way the settings database does:
// agent_profiles.agent_id references an agent row by its generated ID, and the
// row's name is the agent type.
type agentRowSettingsStore struct {
	settingsstore.Repository
	agents   map[string]*settingsmodels.Agent
	profiles map[string]*settingsmodels.AgentProfile
}

func (s *agentRowSettingsStore) GetAgentProfile(_ context.Context, id string) (*settingsmodels.AgentProfile, error) {
	return s.profiles[id], nil
}

func (s *agentRowSettingsStore) GetAgent(_ context.Context, id string) (*settingsmodels.Agent, error) {
	return s.agents[id], nil
}

func (s *agentRowSettingsStore) ListAgents(context.Context) ([]*settingsmodels.Agent, error) {
	rows := make([]*settingsmodels.Agent, 0, len(s.agents))
	for _, agent := range s.agents {
		rows = append(rows, agent)
	}
	return rows, nil
}

func (s *agentRowSettingsStore) ListAgentProfiles(_ context.Context, agentID string) ([]*settingsmodels.AgentProfile, error) {
	var profiles []*settingsmodels.AgentProfile
	for _, profile := range s.profiles {
		if profile.AgentID == agentID {
			profiles = append(profiles, profile)
		}
	}
	return profiles, nil
}

// recordingProxyResolver answers for one agent type and records which type the
// adapter asked about.
type recordingProxyResolver struct {
	wantType string
	client   agentusage.ProviderUsageClient
	asked    []string
}

func (r *recordingProxyResolver) Resolve(
	_ *settingsmodels.AgentProfile,
	agentType string,
) (agentusage.ProviderUsageClient, string, bool) {
	r.asked = append(r.asked, agentType)
	if agentType != r.wantType {
		return nil, "", false
	}
	return r.client, "recording-" + agentType, true
}

func codexRowStore() *agentRowSettingsStore {
	return &agentRowSettingsStore{
		agents: map[string]*settingsmodels.Agent{
			"925465c2-row": {ID: "925465c2-row", Name: codexACPAgentID},
		},
		profiles: map[string]*settingsmodels.AgentProfile{
			"luna": {ID: "luna", AgentID: "925465c2-row", Model: "gpt-6-luna"},
		},
	}
}

func weeklyUsage(now time.Time, pct float64) *agentusage.ProviderUsage {
	return &agentusage.ProviderUsage{
		Provider: "openai",
		Windows: []agentusage.UtilizationWindow{{
			Label: "7-day", UtilizationPct: pct, DurationSeconds: 7 * 24 * 3600,
			StartAt: now.Add(-24 * time.Hour), ResetAt: now.Add(6 * 24 * time.Hour),
		}},
		FetchedAt: now,
	}
}

// TestUsageResolvesTheAgentTypeFromTheAgentRow pins the cause of every profile
// reporting unknown usage: the profile's agent_id is the agent row's ID, so the
// adapter must read the type from the row instead of using the ID as a type.
func TestUsageResolvesTheAgentTypeFromTheAgentRow(t *testing.T) {
	now := time.Now()
	proxy := &recordingProxyResolver{
		wantType: codexACPAgentID, client: &fakeUsageClient{usage: weeklyUsage(now, 22)},
	}
	adapter := &usageProviderAdapter{
		svc: agentusage.NewUsageService(), settingsStore: codexRowStore(), proxyResolver: proxy,
	}

	result := adapter.ProfileUsage(context.Background(), "luna")
	if result.State != profileUsageOK || result.Usage == nil || len(result.Usage.Windows) != 1 {
		t.Fatalf("ProfileUsage = %#v, want the account reading", result)
	}
	if len(proxy.asked) != 1 || proxy.asked[0] != codexACPAgentID {
		t.Fatalf("resolver asked about %v, want the agent row's type", proxy.asked)
	}
	if usage, err := adapter.GetUsage(context.Background(), "luna"); err != nil || usage == nil {
		t.Fatalf("GetUsage = %#v, %v; want Office utilization to read the same account", usage, err)
	}
}

func TestUnreadableAccountIsReportedNotZero(t *testing.T) {
	failing := &failingUsageClient{err: &agentusage.FetchError{
		Provider: "openai", Reason: agentusage.FailureUnauthorized, Status: 401,
	}}
	adapter := &usageProviderAdapter{
		svc: agentusage.NewUsageService(), settingsStore: codexRowStore(),
		proxyResolver: &recordingProxyResolver{wantType: codexACPAgentID, client: failing},
	}
	result := adapter.ProfileUsage(context.Background(), "luna")
	if result.State != profileUsageUnavailable || result.Reason != agentusage.FailureUnauthorized || result.Status != 401 {
		t.Fatalf("ProfileUsage = %#v, want an unavailable state with its reason", result)
	}
}

type failingUsageClient struct{ err error }

func (f *failingUsageClient) FetchUsage(context.Context) (*agentusage.ProviderUsage, error) {
	return nil, f.err
}

// TestRowWithoutManualWindowsRanksOnTheProfileUsage pins that usage belongs to
// the concrete profile: a row that never chose a source still ranks on the
// account reading instead of staying unknown.
func TestRowWithoutManualWindowsRanksOnTheProfileUsage(t *testing.T) {
	now := time.Now()
	adapter := &usageProviderAdapter{
		svc: agentusage.NewUsageService(), settingsStore: codexRowStore(),
		proxyResolver: &recordingProxyResolver{
			wantType: codexACPAgentID, client: &fakeUsageClient{usage: weeklyUsage(now, 22)},
		},
	}
	snapshot := newDynamicUsageSnapshot(adapter, nil, func() time.Time { return now })
	for _, source := range []dynamicruntime.UsageSource{
		dynamicruntime.UsageNone, dynamicruntime.UsageAutomatic, "",
	} {
		scores, err := snapshot.UsageSnapshot(context.Background(), dynamicruntime.Profile{
			Candidates: []dynamicruntime.Candidate{automaticCandidate("luna", source, "")},
		})
		if err != nil {
			t.Fatalf("UsageSnapshot: %v", err)
		}
		if score := scores["luna"]; !score.Known || score.UsageFraction != 0.22 {
			t.Fatalf("source %q score = %#v, want the profile's 22%% weekly usage", source, score)
		}
	}
}

func TestExhaustedWindowWithoutResetRanksAsBusy(t *testing.T) {
	now := time.Now()
	usage := &agentusage.ProviderUsage{
		Provider: "llmgateway", FetchedAt: now,
		Windows: []agentusage.UtilizationWindow{{Label: "monthly", UtilizationPct: 100, LimitReached: true}},
	}
	windows := applicableAutomaticWindows("deepseek", usage)
	if len(windows) != 1 || !windows[0].Exhausted {
		t.Fatalf("windows = %#v, want the exhausted account window kept", windows)
	}
	score := dynamicruntime.PaceFromWindows(now, windows)
	if !score.Known || score.Pace < 1/0.05 {
		t.Fatalf("score = %#v, want a known busiest pace", score)
	}

	usage.Windows[0] = agentusage.UtilizationWindow{Label: "monthly", UtilizationPct: 40}
	if windows := applicableAutomaticWindows("deepseek", usage); len(windows) != 0 {
		t.Fatalf("windows = %#v, want an unexhausted window without reset skipped", windows)
	}
}

func TestWindowsForClassKeepsOnlyTheModelsQuota(t *testing.T) {
	usage := &agentusage.ProviderUsage{Windows: []agentusage.UtilizationWindow{
		{Label: "monthly"},
		{Label: "7-day premium", Scope: agentusage.WindowScopePremiumModels},
		{Label: "1-day", Scope: agentusage.WindowScopeFreeModels},
	}}
	free := windowsForClass(usage, agentusage.ModelClassFree)
	if len(free.Windows) != 2 || free.Windows[1].Label != "1-day" {
		t.Fatalf("free model windows = %#v, want account-wide and free-model quotas", free.Windows)
	}
	if len(usage.Windows) != 3 {
		t.Fatal("filtering must not modify the shared account reading")
	}
	unknown := windowsForClass(usage, agentusage.ModelClassUnknown)
	if len(unknown.Windows) != 1 {
		t.Fatalf("unclassified model windows = %#v, want only the account-wide window", unknown.Windows)
	}
}

func TestAccountProfileIDsGroupsProfilesOnOneAccount(t *testing.T) {
	store := &agentRowSettingsStore{
		agents: map[string]*settingsmodels.Agent{"oc-row": {ID: "oc-row", Name: openCodeACPAgentID}},
		profiles: map[string]*settingsmodels.AgentProfile{
			"zen-a": {ID: "zen-a", AgentID: "oc-row", Model: "opencode/ling-free"},
			"zen-b": {ID: "zen-b", AgentID: "oc-row", Model: "opencode/big-pickle"},
			"or-a":  {ID: "or-a", AgentID: "oc-row", Model: "openrouter/openrouter/free"},
		},
	}
	adapter := &usageProviderAdapter{
		svc: agentusage.NewUsageService(), settingsStore: store,
		bindings: &usageBindingResolver{home: t.TempDir(), getenv: func(string) string { return "" }},
	}
	ids, err := adapter.AccountProfileIDs(context.Background(), "zen-a")
	if err != nil {
		t.Fatalf("AccountProfileIDs: %v", err)
	}
	if len(ids) != 2 || !containsID(ids, "zen-a") || !containsID(ids, "zen-b") {
		t.Fatalf("ids = %v, want both Zen profiles and not the OpenRouter one", ids)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestProfileUsageDTO_PropagatesStale(t *testing.T) {
	now := time.Now().Add(-5 * time.Minute)
	dto := profileUsageDTO("prof-1", profileUsage{
		State: profileUsageOK,
		Usage: &agentusage.ProviderUsage{
			Provider:  "anthropic",
			Plan:      "max",
			FetchedAt: now,
			Stale:     true,
			Windows: []agentusage.UtilizationWindow{
				{Label: "5-hour", UtilizationPct: 45.0},
			},
		},
	})
	if !dto.Stale {
		t.Error("expected dto.Stale to be true")
	}
	if dto.FetchedAt == nil || !dto.FetchedAt.Equal(now) {
		t.Errorf("dto.FetchedAt = %v, want %v", dto.FetchedAt, now)
	}
}
