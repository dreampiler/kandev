package backendapp

import (
	"context"
	"errors"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/settings/controller"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
)

type memoryProviderLimitStore struct {
	limits map[string]dynamicruntime.ProviderLimit
}

func (s *memoryProviderLimitStore) ListProviderLimits(context.Context) ([]dynamicruntime.ProviderLimit, error) {
	limits := make([]dynamicruntime.ProviderLimit, 0, len(s.limits))
	for _, limit := range s.limits {
		limits = append(limits, limit)
	}
	return limits, nil
}

func (s *memoryProviderLimitStore) SaveProviderLimit(_ context.Context, limit dynamicruntime.ProviderLimit) error {
	s.limits[limit.Provider] = limit
	return nil
}

type providerProfilesRepo struct {
	settingsstore.Repository
	profiles []*settingsmodels.AgentProfile
}

func (r providerProfilesRepo) ListAgents(context.Context) ([]*settingsmodels.Agent, error) {
	return []*settingsmodels.Agent{{ID: "opencode"}}, nil
}

func (r providerProfilesRepo) ListAgentProfiles(context.Context, string) ([]*settingsmodels.AgentProfile, error) {
	return r.profiles, nil
}

func TestProviderLimitServiceListsProvidersFromProfiles(t *testing.T) {
	deleted := time.Now()
	repo := providerProfilesRepo{profiles: []*settingsmodels.AgentProfile{
		{ID: "go-kimi", Enabled: true, Model: "opencode-go/kimi-k2"},
		{ID: "go-free", Enabled: true, Model: "opencode-go/minimax-m2.5-free"},
		{ID: "devpass", Enabled: true, Model: "llmgateway/claude-sonnet"},
		{ID: "claude", Enabled: true, Model: "sonnet"},
		{ID: "gone", Enabled: true, Model: "openrouter/x", DeletedAt: &deleted},
	}}
	anchor := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
	store := &memoryProviderLimitStore{limits: map[string]dynamicruntime.ProviderLimit{
		"opencode-go": {Provider: "opencode-go", MonthlyResetAt: &anchor},
	}}
	service := newProviderLimitService(store, repo, nil)
	service.now = func() time.Time { return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) }

	providers, err := service.ListProviderLimits(context.Background())
	if err != nil {
		t.Fatalf("ListProviderLimits: %v", err)
	}
	if len(providers) != 2 || providers[0].Provider != "llmgateway" || providers[1].Provider != "opencode-go" {
		t.Fatalf("providers = %#v", providers)
	}
	goLimit := providers[1]
	if goLimit.ProfileCount != 2 || !goLimit.ModelScoped || goLimit.NextMonthlyResetSource != "manual" ||
		goLimit.NextMonthlyReset == nil || !goLimit.NextMonthlyReset.Equal(time.Date(2026, 10, 30, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("opencode-go = %#v", goLimit)
	}
	if providers[0].ModelScoped {
		t.Fatal("an account-billed provider must not be reported as model scoped")
	}
}

func TestProviderLimitServiceValidatesUpdates(t *testing.T) {
	store := &memoryProviderLimitStore{limits: map[string]dynamicruntime.ProviderLimit{}}
	service := newProviderLimitService(store, providerProfilesRepo{}, nil)
	block := time.Date(2026, 10, 7, 14, 20, 0, 0, time.UTC)

	if _, err := service.UpdateProviderLimit(context.Background(), "Bad Provider!", controller.UpdateProviderLimitRequest{}); !errors.Is(err, controller.ErrInvalidProviderLimit) {
		t.Fatalf("invalid provider error = %v", err)
	}
	if _, err := service.UpdateProviderLimit(context.Background(), "llmgateway", controller.UpdateProviderLimitRequest{
		MonthlyResetAt: &block, MonthlyResetTimezone: "Mars/Olympus",
	}); !errors.Is(err, controller.ErrInvalidProviderLimit) {
		t.Fatalf("invalid timezone error = %v", err)
	}
	saved, err := service.UpdateProviderLimit(context.Background(), "LLMGateway/", controller.UpdateProviderLimitRequest{
		BlockUntil: &block, MonthlyResetTimezone: "UTC",
	})
	if err != nil {
		t.Fatalf("UpdateProviderLimit: %v", err)
	}
	stored := store.limits["llmgateway"]
	if saved.Provider != "llmgateway" || stored.BlockUntil == nil || !stored.BlockUntil.Equal(block) ||
		stored.MonthlyResetTimezone != "" {
		t.Fatalf("saved = %#v, stored = %#v", saved, stored)
	}
}
