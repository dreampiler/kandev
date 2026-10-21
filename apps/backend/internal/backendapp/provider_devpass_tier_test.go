package backendapp

import (
	"context"
	"testing"
	"time"

	agentregistry "github.com/kandev/kandev/internal/agent/registry"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

type devPassSettingsRepo struct {
	settingsstore.Repository
	profile *settingsmodels.AgentProfile
	agent   *settingsmodels.Agent
}

func (r devPassSettingsRepo) GetAgentProfile(context.Context, string) (*settingsmodels.AgentProfile, error) {
	return r.profile, nil
}

func (r devPassSettingsRepo) GetAgent(context.Context, string) (*settingsmodels.Agent, error) {
	return r.agent, nil
}

func (r devPassSettingsRepo) ListAgents(context.Context) ([]*settingsmodels.Agent, error) {
	return []*settingsmodels.Agent{r.agent}, nil
}

func (r devPassSettingsRepo) ListAgentProfiles(context.Context, string) ([]*settingsmodels.AgentProfile, error) {
	return []*settingsmodels.AgentProfile{r.profile}, nil
}

type devPassTierStore struct {
	owner agentusage.AccountTierConfig
}

func (s devPassTierStore) LoadAccountTierConfig(context.Context, string) (agentusage.AccountTierConfig, agentusage.AccountTierConfig, error) {
	return s.owner, agentusage.AccountTierConfig{}, nil
}
func (devPassTierStore) SaveAccountTierOverrides(context.Context, string, agentusage.AccountTierConfig) error {
	return nil
}
func (devPassTierStore) SaveAccountTierLearned(context.Context, string, agentusage.AccountTierConfig) error {
	return nil
}
func (devPassTierStore) CountLimitActiveTurns(context.Context, []string, string) (int64, error) {
	return 0, nil
}
func (devPassTierStore) GetManualWindowUsageForProfiles(context.Context, []string, time.Time, time.Time) (sqliterepo.ManualWindowUsage, error) {
	return sqliterepo.ManualWindowUsage{}, nil
}
func (devPassTierStore) InsertUsageLimitObservation(context.Context, sqliterepo.UsageLimitObservation) error {
	return nil
}
func (devPassTierStore) ListUsageLimitObservationsSince(context.Context, time.Time) ([]sqliterepo.UsageLimitObservation, error) {
	return nil, nil
}

func newDevPassService(owner agentusage.AccountTierConfig) *providerTierLimitService {
	profile := &settingsmodels.AgentProfile{
		ID: "devpass", Name: "DevPass", AgentID: "agent-1",
		Model: "llmgateway/claude-sonnet", Enabled: true,
	}
	agent := &settingsmodels.Agent{ID: "agent-1", Name: openCodeACPAgentID}
	repo := devPassSettingsRepo{profile: profile, agent: agent}
	usage := newUsageProviderAdapter(repo, agentregistry.NewRegistry(nil))
	return newProviderTierLimitService(usage, devPassTierStore{owner: owner})
}

func TestDevPassNeedsAnExplicitProfileTierMapping(t *testing.T) {
	owner := agentusage.AccountTierConfig{ProfileTiers: map[string]agentusage.BillingTier{
		"devpass": agentusage.TierSubscription,
	}}
	service := newDevPassService(owner)
	c, err := service.contextFor(context.Background(), "devpass")
	if err != nil {
		t.Fatal(err)
	}
	if c.tier != agentusage.TierSubscription {
		t.Fatalf("tier = %s, want subscription after the owner mapping", c.tier)
	}
	if c.defaults.RequestsPerMinute == nil || *c.defaults.RequestsPerMinute != 120 ||
		c.defaults.MaxConcurrentTurns == nil || *c.defaults.MaxConcurrentTurns != 50 {
		t.Fatalf("DevPass defaults = %#v, want 120/min and 50 turns", c.defaults)
	}
}

func TestLLMGatewayWithoutMappingKeepsThePaidDefault(t *testing.T) {
	service := newDevPassService(agentusage.AccountTierConfig{})
	c, err := service.contextFor(context.Background(), "devpass")
	if err != nil {
		t.Fatal(err)
	}
	if c.tier != agentusage.TierPaid {
		t.Fatalf("tier = %s, want paid until the profile is mapped", c.tier)
	}
	if c.defaults.RequestsPerMinute == nil || *c.defaults.RequestsPerMinute != 600 {
		t.Fatalf("unmapped defaults = %#v, want the paid default", c.defaults)
	}
}
