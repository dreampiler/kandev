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
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/secrets"
	systemsettings "github.com/kandev/kandev/internal/system/settings"
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
	observed      *agentusage.ObservedStore
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
	// Observed marks a value that came from a running agent's own report rather
	// than from the provider's usage API.
	Observed bool
}

// GetUsage implements officeagents.UsageProvider. It returns the account's
// unfiltered reading, or nil when the profile has no readable account. A provider
// failure is still returned as an error even when an observed window stands in for
// the value, so a caller can tell a real reading from a substituted one.
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

// ObserveProviderWindow records one subscription window a running agent reported
// about its own account. It implements lifecycle.ProviderWindowObserver, and is a
// no-op for a profile Kandev cannot bind to a readable account.
func (a *usageProviderAdapter) ObserveProviderWindow(profileID string, window streams.RateLimitWindow) {
	if a.observed == nil {
		return
	}
	profile, agentType, ok := a.loadProfile(context.Background(), profileID)
	if !ok {
		return
	}
	binding, ok := a.bindingFor(profile, agentType)
	if !ok || binding.client == nil || binding.cacheKey == "" {
		return
	}
	err := a.observed.Record(binding.cacheKey, agentusage.ObservedWindow{
		Provider:    window.Provider,
		WindowType:  window.WindowType,
		Utilization: window.Utilization,
		ResetsAt:    window.ResetsAt,
		Status:      window.Status,
		Overage:     window.Overage,
		ObservedAt:  window.ObservedAt,
	})
	a.logPersistFailure(profileID, binding, err)
}

// logPersistFailure reports a failed observation write once per account and
// interval. The observation itself is already recorded in memory, so a failed
// write only costs durability across restarts.
func (a *usageProviderAdapter) logPersistFailure(profileID string, binding usageBinding, err error) {
	if err == nil || a.log == nil || !a.shouldLogBounded(observedLogPrefix+binding.cacheKey) {
		return
	}
	a.log.Warn("usage.observed_persist_failed",
		zap.String("profile_id", profileID),
		zap.String("account", binding.accountKind()),
		zap.Error(err),
	)
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
	usage, err := a.fetch(ctx, profile.ID, binding)
	if err != nil {
		// The provider read failed. The bounded reason is still reported, and a
		// substituted observation still counts as a value rather than as usage
		// the account does not have.
		result.Reason, result.Status = agentusage.FailureOf(err)
		if usage == nil {
			result.State = profileUsageUnavailable
			return result
		}
		result.Source = usageSourceAgentStream
	}
	if binding.class != nil {
		result.ModelClass = binding.class(ctx)
	}
	result.State = profileUsageOK
	result.Observed = usage != nil && usage.Observed
	result.Usage = windowsForClass(usage, result.ModelClass)
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

// fetch reads the account's usage through the shared cache and decorates the
// result with whatever the account's agents reported about themselves.
//
// The merge sits outside the provider client and the cache on purpose: the
// provider's own success and failure, its Retry-After hint and its exponential
// backoff all stay exactly as the cache recorded them, and a substituted
// observation is never counted as a successful re-query. The provider error is
// still returned alongside the decorated value.
func (a *usageProviderAdapter) fetch(
	ctx context.Context,
	profileID string,
	binding usageBinding,
) (*agentusage.ProviderUsage, error) {
	a.svc.Register(profileID, binding.client, binding.cacheKey)
	reading, observed := a.observedReading(binding)
	usage, err := a.svc.GetUsage(ctx, profileID)
	if err != nil && ctx.Err() == nil {
		a.logFailure(profileID, binding, err)
	}
	merged, _ := agentusage.MergeObserved(usage, err, reading, observed)
	return merged, err
}

// observedReading returns the account's latest usable windows, if any. The
// account is the one the cache already groups by, so profiles sharing an account
// also share one observation.
func (a *usageProviderAdapter) observedReading(binding usageBinding) (agentusage.ObservedReading, bool) {
	if a.observed == nil || binding.cacheKey == "" {
		return agentusage.ObservedReading{}, false
	}
	return a.observed.Latest(binding.cacheKey)
}

// logFailure records a failed read once per account and interval. Only the
// bounded reason and status are logged: provider bodies and credentials never
// reach the log.
func (a *usageProviderAdapter) logFailure(profileID string, binding usageBinding, err error) {
	if a.log == nil || !a.shouldLogBounded(binding.cacheKey) {
		return
	}
	reason, status := agentusage.FailureOf(err)
	a.log.Warn("usage.fetch_failed",
		zap.String("profile_id", profileID),
		zap.String("account", binding.accountKind()),
		zap.String("reason", string(reason)),
		zap.Int("status", status),
	)
}

// shouldLogBounded reports whether one account's condition is due for a log
// line, so a repeatedly failing account stays visible without flooding the log
// on every preview, selection or observation.
func (a *usageProviderAdapter) shouldLogBounded(cacheKey string) bool {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.lastLogged == nil {
		a.lastLogged = make(map[string]time.Time)
	}
	last, seen := a.lastLogged[cacheKey]
	if seen && time.Since(last) < usageFailureLogInterval {
		return false
	}
	a.lastLogged[cacheKey] = time.Now()
	return true
}

// accountKind is the provider part of the account key, which is safe to log
// because it names the provider and not the credential location.
func (b usageBinding) accountKind() string {
	kind, _, _ := strings.Cut(b.accountKey, ":")
	return kind
}

// newUsageProviderAdapter creates the adapter shared by Office utilization, the
// dynamic selection engine and the settings views, so all of them read one cache
// and one set of observed windows.
func newUsageProviderAdapter(
	settingsStore settingsstore.Repository,
	log *logger.Logger,
	openRouterDailyLimit int,
	secretStore secrets.SecretStore,
	systemSettings *systemsettings.Store,
) *usageProviderAdapter {
	return &usageProviderAdapter{
		svc:           agentusage.NewUsageService(),
		settingsStore: settingsStore,
		proxyResolver: defaultUsageProxyResolver(),
		bindings:      newUsageBindingResolver(openRouterDailyLimit, secretStore),
		observed:      agentusage.NewObservedStore(settingsObservedPersistence{store: systemSettings}),
		log:           log,
	}
}

// observedLogPrefix keeps an observation write failure from suppressing the
// provider read failure logged under the same account.
const observedLogPrefix = "observed:"

// settingsObservedPersistence stores observed windows in the install-wide
// settings table, which needs no new table or migration and is already cleared by
// a factory reset. Each call is one indexed row read or write, so it runs on a
// background context rather than borrowing a request's lifetime.
type settingsObservedPersistence struct {
	store *systemsettings.Store
}

func (p settingsObservedPersistence) Load(key string) ([]byte, bool, error) {
	if p.store == nil {
		return nil, false, nil
	}
	return p.store.Get(context.Background(), key)
}

func (p settingsObservedPersistence) Save(key string, value []byte) error {
	if p.store == nil {
		return nil
	}
	return p.store.Save(context.Background(), key, value)
}
