package backendapp

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	"github.com/kandev/kandev/internal/common/logger"
)

// usageFailureLogInterval bounds how often one account's failing usage read is
// logged, so a broken credential is visible without flooding the log on every
// preview or selection.
const usageFailureLogInterval = 15 * time.Minute

// Profile usage states. Each names why a reading is or is not available, so a
// client can explain an unknown value instead of showing a bare blank.
const (
	profileUsageOK          = "ok"
	profileUsageUnavailable = "unavailable"
	profileUsageUnsupported = "unsupported"
	profileUsageNoUsageAPI  = "no_usage_api"
)

// usageProviderAdapter answers subscription usage for concrete agent profiles.
// It resolves the profile's agent type from its agent row, binds the profile to
// the account its host-run agent authenticates with, and reads that account
// through the cached UsageService.
type usageProviderAdapter struct {
	svc           *agentusage.UsageService
	settingsStore settingsstore.Repository
	proxyResolver usageProxyResolver
	bindings      *usageBindingResolver
	log           *logger.Logger

	logMu      sync.Mutex
	lastLogged map[string]time.Time

	accounts accountIndex
}

// profileUsage is one profile's reading, filtered to the windows that limit its
// own model.
type profileUsage struct {
	State      string
	Reason     agentusage.FetchFailure
	Status     int
	Source     string
	AccountKey string
	ModelID    string
	ModelClass agentusage.ModelClass
	Usage      *agentusage.ProviderUsage
}

// GetUsage implements officeagents.UsageProvider. It returns the account's
// unfiltered reading, or nil when the profile has no readable account.
func (a *usageProviderAdapter) GetUsage(ctx context.Context, profileID string) (*agentusage.ProviderUsage, error) {
	profile, agentType, ok := a.loadProfile(ctx, profileID)
	if !ok {
		return nil, nil
	}
	binding, ok := a.bindingFor(profile, agentType)
	if !ok || binding.client == nil {
		return nil, nil
	}
	return a.fetch(ctx, profileID, binding)
}

// ProfileUsage reads one profile's own usage. Every outcome carries a state, so
// an unreadable account is reported rather than presented as zero usage.
func (a *usageProviderAdapter) ProfileUsage(ctx context.Context, profileID string) profileUsage {
	profile, agentType, ok := a.loadProfile(ctx, profileID)
	if !ok {
		return profileUsage{State: profileUsageUnsupported}
	}
	return a.usageForProfile(ctx, profile, agentType)
}

func (a *usageProviderAdapter) usageForProfile(
	ctx context.Context,
	profile *settingsmodels.AgentProfile,
	agentType string,
) profileUsage {
	binding, ok := a.bindingFor(profile, agentType)
	if !ok {
		return profileUsage{State: profileUsageUnsupported}
	}
	result := profileUsage{Source: binding.source, AccountKey: binding.accountKey, ModelID: binding.modelID}
	if binding.client == nil {
		result.State = profileUsageNoUsageAPI
		return result
	}
	observed, err := a.fetch(ctx, profile.ID, binding)
	if err != nil {
		result.State = profileUsageUnavailable
		result.Reason, result.Status = agentusage.FailureOf(err)
		return result
	}
	if binding.class != nil {
		result.ModelClass = binding.class(ctx)
	}
	result.State = profileUsageOK
	result.Usage = windowsForClass(observed, result.ModelClass)
	return result
}

// windowsForClass keeps the windows that limit a model of the given class. The
// account reading is shared by every profile on the account, so it is copied
// rather than filtered in place.
func windowsForClass(observed *agentusage.ProviderUsage, class agentusage.ModelClass) *agentusage.ProviderUsage {
	if observed == nil {
		return nil
	}
	filtered := *observed
	filtered.Windows = make([]agentusage.UtilizationWindow, 0, len(observed.Windows))
	for _, window := range observed.Windows {
		if window.Scope.AppliesTo(class) {
			filtered.Windows = append(filtered.Windows, window)
		}
	}
	return &filtered
}

// loadProfile resolves the profile and its agent type. agent_profiles.agent_id
// references the agent row, whose name is the registry's agent type; an ID that
// is not a row is treated as the type itself.
func (a *usageProviderAdapter) loadProfile(
	ctx context.Context,
	profileID string,
) (*settingsmodels.AgentProfile, string, bool) {
	profile, err := a.settingsStore.GetAgentProfile(ctx, profileID)
	if err != nil || profile == nil {
		return nil, "", false
	}
	return profile, a.agentType(ctx, profile.AgentID), true
}

func (a *usageProviderAdapter) agentType(ctx context.Context, agentID string) string {
	agent, err := a.settingsStore.GetAgent(ctx, agentID)
	if err == nil && agent != nil && agent.Name != "" {
		return agent.Name
	}
	return agentID
}

func (a *usageProviderAdapter) bindingFor(profile *settingsmodels.AgentProfile, agentType string) (usageBinding, bool) {
	if a.proxyResolver != nil {
		if client, cacheKey, ok := a.proxyResolver.Resolve(profile, agentType); ok {
			return usageBinding{
				client: client, cacheKey: cacheKey, source: usageSourceProxy,
				accountKey: "proxy:" + cacheKey, modelID: profile.Model,
			}, true
		}
	}
	if a.bindings == nil {
		return usageBinding{}, false
	}
	return a.bindings.Resolve(profile, agentType)
}

func (a *usageProviderAdapter) fetch(
	ctx context.Context,
	profileID string,
	binding usageBinding,
) (*agentusage.ProviderUsage, error) {
	a.svc.Register(profileID, binding.client, binding.cacheKey)
	observed, err := a.svc.GetUsage(ctx, profileID)
	if err != nil && ctx.Err() == nil {
		a.logFailure(profileID, binding, err)
	}
	return observed, err
}

// logFailure records a failed read once per account and interval. Only the
// bounded reason and status are logged: provider bodies and credentials never
// reach the log.
func (a *usageProviderAdapter) logFailure(profileID string, binding usageBinding, err error) {
	if a.log == nil {
		return
	}
	a.logMu.Lock()
	if a.lastLogged == nil {
		a.lastLogged = make(map[string]time.Time)
	}
	last, seen := a.lastLogged[binding.cacheKey]
	if seen && time.Since(last) < usageFailureLogInterval {
		a.logMu.Unlock()
		return
	}
	a.lastLogged[binding.cacheKey] = time.Now()
	a.logMu.Unlock()
	reason, status := agentusage.FailureOf(err)
	a.log.Warn("usage.fetch_failed",
		zap.String("profile_id", profileID),
		zap.String("account", binding.accountKind()),
		zap.String("reason", string(reason)),
		zap.Int("status", status),
	)
}

// accountKind is the provider part of the account key, which is safe to log
// because it names the provider and not the credential location.
func (b usageBinding) accountKind() string {
	kind, _, _ := strings.Cut(b.accountKey, ":")
	return kind
}

// newUsageProviderAdapter creates the adapter shared by Office utilization, the
// dynamic selection engine and the settings views, so all of them read one
// cache.
func newUsageProviderAdapter(
	settingsStore settingsstore.Repository,
	log *logger.Logger,
) *usageProviderAdapter {
	return &usageProviderAdapter{
		svc:           agentusage.NewUsageService(),
		settingsStore: settingsStore,
		proxyResolver: defaultUsageProxyResolver(),
		bindings:      newUsageBindingResolver(),
		log:           log,
	}
}
