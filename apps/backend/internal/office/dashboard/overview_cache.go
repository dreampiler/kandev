package dashboard

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/kandev/kandev/internal/auth/authn"
)

// overviewCacheTTL is how long one computed overview snapshot is served
// before the next request recomputes it.
const overviewCacheTTL = 20 * time.Second

// overviewCacheMaxEntries bounds the cache; one entry exists per
// (caller, scope), so this only matters on large multi-user installs.
const overviewCacheMaxEntries = 256

type overviewCacheEntry struct {
	snap    *overviewSnapshot
	expires time.Time
}

// overviewCache keeps one snapshot per (caller identity, scope) for a short
// TTL and collapses concurrent misses for the same key into one build. The
// key carries the caller because the workspace list is identity-scoped:
// a shared entry would leak one caller's workspaces to another.
type overviewCache struct {
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]overviewCacheEntry
	group   singleflight.Group
}

func newOverviewCache(ttl time.Duration) *overviewCache {
	return &overviewCache{ttl: ttl, now: time.Now, entries: map[string]overviewCacheEntry{}}
}

func (c *overviewCache) lookup(key string) *overviewSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expires) {
		return nil
	}
	return entry.snap
}

func (c *overviewCache) store(key string, snap *overviewSnapshot) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= overviewCacheMaxEntries {
		var oldestKey string
		var oldest time.Time
		for k, entry := range c.entries {
			if oldestKey == "" || entry.expires.Before(oldest) {
				oldestKey, oldest = k, entry.expires
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = overviewCacheEntry{snap: snap, expires: now.Add(c.ttl)}
}

// get returns the cached snapshot for key or builds it once. The build runs
// on a context detached from the first caller's cancellation (identity and
// other values are kept) so a caller that goes away does not fail the
// others waiting on the same flight.
func (c *overviewCache) get(
	ctx context.Context, key string, build func(context.Context) (*overviewSnapshot, error),
) (*overviewSnapshot, error) {
	if snap := c.lookup(key); snap != nil {
		return snap, nil
	}
	v, err, _ := c.group.Do(key, func() (interface{}, error) {
		if snap := c.lookup(key); snap != nil {
			return snap, nil
		}
		snap, err := build(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		c.store(key, snap)
		return snap, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*overviewSnapshot), nil
}

// overviewCallerKey identifies the caller for cache partitioning. Without a
// real identity (authentication disabled) every request is the same local
// user.
func overviewCallerKey(ctx context.Context) string {
	identity, ok := authn.IdentityFromContext(ctx)
	if !ok || identity.Synthetic || identity.UserID == "" {
		return "local"
	}
	return "user:" + identity.UserID
}
