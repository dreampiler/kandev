package backendapp

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kandev/kandev/internal/agent/agents"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
)

// accountIndexTTL bounds how stale the profile-to-account grouping may be. A
// profile created or retargeted within this interval joins its account's
// window on the next refresh.
const accountIndexTTL = time.Minute

var errUsageAccountUnknown = errors.New("profile has no identifiable usage account")

// boundProfile is one concrete profile with its agent type and account binding.
type boundProfile struct {
	profile   *settingsmodels.AgentProfile
	agentType string
	binding   usageBinding
}

type accountIndex struct {
	mu       sync.Mutex
	builtAt  time.Time
	accounts map[string][]string
}

// AccountProfileIDs lists every concrete profile bound to the same provider
// account as profileID, including profileID itself.
func (a *usageProviderAdapter) AccountProfileIDs(ctx context.Context, profileID string) ([]string, error) {
	profile, agentType, ok := a.loadProfile(ctx, profileID)
	if !ok {
		return nil, errUsageAccountUnknown
	}
	binding, ok := a.bindingFor(profile, agentType)
	if !ok || binding.accountKey == "" {
		return nil, errUsageAccountUnknown
	}
	accounts, err := a.accountGroups(ctx)
	if err != nil {
		return nil, err
	}
	ids := accounts[binding.accountKey]
	for _, id := range ids {
		if id == profileID {
			return ids, nil
		}
	}
	return append(append([]string(nil), ids...), profileID), nil
}

// ListProfileAccountIDs names the provider account of every live concrete
// profile, for a caller that groups profiles by account. It reads the same
// minute-old index the account-scoped counting uses, and returns no credential
// path: the value is the derived account id.
func (a *usageProviderAdapter) ListProfileAccountIDs(ctx context.Context) (map[string]string, error) {
	accounts, err := a.accountGroups(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(accounts))
	for accountKey, profileIDs := range accounts {
		identity := accountIdentityFor(accountKey)
		if identity.ID == "" {
			continue
		}
		for _, profileID := range profileIDs {
			ids[profileID] = identity.ID
		}
	}
	return ids, nil
}

func (a *usageProviderAdapter) accountGroups(ctx context.Context) (map[string][]string, error) {
	a.accounts.mu.Lock()
	defer a.accounts.mu.Unlock()
	if a.accounts.accounts != nil && time.Since(a.accounts.builtAt) < accountIndexTTL {
		return a.accounts.accounts, nil
	}
	profiles, err := a.boundProfiles(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make(map[string][]string)
	for _, bound := range profiles {
		key := bound.binding.accountKey
		accounts[key] = append(accounts[key], bound.profile.ID)
	}
	a.accounts.accounts, a.accounts.builtAt = accounts, time.Now()
	return accounts, nil
}

// boundProfiles resolves every live concrete profile to its account. Dynamic
// profiles are routing documents rather than accounts, so they are skipped.
func (a *usageProviderAdapter) boundProfiles(ctx context.Context) ([]boundProfile, error) {
	agentRows, err := a.settingsStore.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	var bound []boundProfile
	for _, agent := range agentRows {
		if agent == nil || agent.Name == agents.DynamicAgentID {
			continue
		}
		profiles, err := a.settingsStore.ListAgentProfiles(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
		for _, profile := range profiles {
			if profile == nil || profile.DeletedAt != nil {
				continue
			}
			binding, ok := a.bindingFor(profile, agent.Name)
			if !ok || binding.accountKey == "" {
				continue
			}
			bound = append(bound, boundProfile{profile: profile, agentType: agent.Name, binding: binding})
		}
	}
	return bound, nil
}

// prefetchAccounts reads each distinct account once, concurrently, so a list of
// many profiles costs one provider request per account rather than one per
// profile in sequence.
func (a *usageProviderAdapter) prefetchAccounts(ctx context.Context, profiles []boundProfile) {
	seen := make(map[string]bool)
	var wg sync.WaitGroup
	for _, bound := range profiles {
		if bound.binding.client == nil || seen[bound.binding.cacheKey] {
			continue
		}
		seen[bound.binding.cacheKey] = true
		wg.Add(1)
		go func(bound boundProfile) {
			defer wg.Done()
			_, _ = a.fetch(ctx, bound.profile.ID, bound.binding)
			if bound.binding.class != nil {
				_ = bound.binding.class(ctx)
			}
		}(bound)
	}
	wg.Wait()
}
