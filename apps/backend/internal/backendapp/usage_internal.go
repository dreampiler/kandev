package backendapp

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/common/logger"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// Internal accumulation windows. Kandev's own ledger is summed over trailing
// windows ending now; the ranking window is one day because the undisclosed
// free quotas this serves reset daily.
const (
	internalRankingWindow = 24 * time.Hour
	internalCacheTTL      = 30 * time.Second
	window5h              = 5 * time.Hour
	windowWeek            = 7 * 24 * time.Hour
)

// accountUsageReader sums recorded usage for the profiles on one account.
type accountUsageReader struct {
	usage  *usageProviderAdapter
	ledger accountWindowTotalsReader

	mu    sync.Mutex
	cache map[string]cachedInternal
}

type cachedInternal struct {
	at    time.Time
	value dynamicruntime.InternalUsage
}

func newAccountUsageReader(usage *usageProviderAdapter, ledger accountWindowTotalsReader) *accountUsageReader {
	return &accountUsageReader{usage: usage, ledger: ledger, cache: make(map[string]cachedInternal)}
}

// accountProfiles lists the profiles whose usage counts against the same
// account as profileID. A profile without an identifiable account counts alone.
func (r *accountUsageReader) accountProfiles(ctx context.Context, profileID string) []string {
	if r.usage != nil {
		if ids, err := r.usage.AccountProfileIDs(ctx, profileID); err == nil && len(ids) > 0 {
			return ids
		}
	}
	return []string{profileID}
}

// totals sums the account's recorded usage over [now-span, now).
func (r *accountUsageReader) totals(
	ctx context.Context,
	profileIDs []string,
	span time.Duration,
	now time.Time,
) (sqliterepo.ManualWindowUsage, error) {
	return r.ledger.GetManualWindowUsageForProfiles(ctx, profileIDs, now.Add(-span), now)
}

// Internal returns the candidate's account usage over the ranking window. A
// short cache keeps one decision from re-reading the ledger per candidate.
func (r *accountUsageReader) Internal(ctx context.Context, profileID string, now time.Time) dynamicruntime.InternalUsage {
	if r == nil || r.ledger == nil {
		return dynamicruntime.InternalUsage{}
	}
	ids := r.accountProfiles(ctx, profileID)
	key := accountCacheKey(ids)
	r.mu.Lock()
	cached, ok := r.cache[key]
	r.mu.Unlock()
	if ok && now.Sub(cached.at) < internalCacheTTL && !now.Before(cached.at) {
		return cached.value
	}
	totals, err := r.totals(ctx, ids, internalRankingWindow, now)
	if err != nil {
		return dynamicruntime.InternalUsage{}
	}
	value := dynamicruntime.InternalUsage{Known: true, Turns: totals.EventCount, Tokens: totals.TokensTotal}
	r.mu.Lock()
	r.cache[key] = cachedInternal{at: now, value: value}
	r.mu.Unlock()
	return value
}

func accountCacheKey(ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// usageLimitLedger stores limit observations.
type usageLimitLedger interface {
	InsertUsageLimitObservation(ctx context.Context, observation sqliterepo.UsageLimitObservation) error
}

// usageLimitRecorder implements dynamic.LimitObserver. It snapshots the
// account's recorded usage when a candidate hits a limit, off the routing path,
// so the decision that reported the failure is never delayed by the ledger.
type usageLimitRecorder struct {
	accounts *accountUsageReader
	store    usageLimitLedger
	log      *logger.Logger
	wg       sync.WaitGroup
}

func newUsageLimitRecorder(accounts *accountUsageReader, store usageLimitLedger, log *logger.Logger) *usageLimitRecorder {
	return &usageLimitRecorder{accounts: accounts, store: store, log: log}
}

// ObserveLimit implements dynamic.LimitObserver.
func (r *usageLimitRecorder) ObserveLimit(_ context.Context, profileID string, code routingerr.Code, at time.Time) {
	if r == nil || r.store == nil || r.accounts == nil || r.accounts.ledger == nil {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := r.record(ctx, profileID, code, at); err != nil && r.log != nil {
			r.log.Warn("usage.limit_observation_failed", zap.String("profile_id", profileID), zap.Error(err))
		}
	}()
}

// wait blocks until pending observations are stored; tests use it.
func (r *usageLimitRecorder) wait() { r.wg.Wait() }

func (r *usageLimitRecorder) record(ctx context.Context, profileID string, code routingerr.Code, at time.Time) error {
	ids := r.accounts.accountProfiles(ctx, profileID)
	observation := sqliterepo.UsageLimitObservation{
		ID: uuid.NewString(), ExecutionProfileID: profileID, Code: string(code), ObservedAt: at,
		AccountKey: r.accounts.accountKey(ctx, profileID),
	}
	short, err := r.accounts.totals(ctx, ids, window5h, at)
	if err != nil {
		return err
	}
	day, err := r.accounts.totals(ctx, ids, internalRankingWindow, at)
	if err != nil {
		return err
	}
	week, err := r.accounts.totals(ctx, ids, windowWeek, at)
	if err != nil {
		return err
	}
	observation.Turns5h, observation.Tokens5h = short.EventCount, short.TokensTotal
	observation.TurnsDay, observation.TokensDay, observation.CostSubcentsDay = day.EventCount, day.TokensTotal, day.CostSubcents
	observation.TurnsWeek, observation.TokensWeek = week.EventCount, week.TokensTotal
	return r.store.InsertUsageLimitObservation(ctx, observation)
}

// accountKey identifies the profile's account for grouping observations. A
// profile without a binding is its own account.
func (r *accountUsageReader) accountKey(ctx context.Context, profileID string) string {
	if r.usage != nil {
		if profile, agentType, ok := r.usage.loadProfile(ctx, profileID); ok {
			if binding, bound := r.usage.bindingFor(profile, agentType); bound && binding.accountKey != "" {
				return binding.accountKey
			}
		}
	}
	return "profile:" + profileID
}

// routeSelectionHistory adapts the route attempt log to dynamic.SelectionHistory.
type routeSelectionHistory struct {
	repo interface {
		LastDynamicRouteSelections(ctx context.Context, logicalProfileID string) (map[string]time.Time, error)
	}
}

// LastSelections implements dynamic.SelectionHistory.
func (h routeSelectionHistory) LastSelections(ctx context.Context, logicalProfileID string) (map[string]time.Time, error) {
	if h.repo == nil {
		return nil, nil
	}
	return h.repo.LastDynamicRouteSelections(ctx, logicalProfileID)
}
