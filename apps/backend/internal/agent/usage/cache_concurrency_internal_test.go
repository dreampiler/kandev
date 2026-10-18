package usage

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestUsageCacheConcurrentKeysAndFailures(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cache := NewUsageCache()
				started, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				cause := refusedUsage()
				reading := &ProviderUsage{FetchedAt: time.Now()}
				fetch := func(context.Context) (*ProviderUsage, error) {
					if calls.Add(1) == 1 {
						close(started)
					}
					<-release
					if fail {
						return nil, cause
					}
					return reading, nil
				}
				done := make(chan struct{}, 8)
				for range 8 {
					go func() {
						got, err := cache.GetOrFetch(t.Context(), "shared", fetch)
						if fail {
							if got != nil || !errors.Is(err, cause) {
								t.Errorf("lost shared failure: %v %v", got, err)
							}
						} else if got != reading || err != nil {
							t.Errorf("lost shared success: %v %v", got, err)
						}
						done <- struct{}{}
					}()
				}
				<-started
				synctest.Wait()
				// Another key must complete while the first key's provider is blocked.
				other, err := cache.GetOrFetch(t.Context(), "other", func(context.Context) (*ProviderUsage, error) { return reading, nil })
				if other != reading || err != nil {
					t.Fatalf("independent key blocked or failed: %v %v", other, err)
				}
				close(release)
				for range 8 {
					<-done
				}
				if calls.Load() != 1 {
					t.Fatalf("same-key misses made %d provider calls", calls.Load())
				}
			})
		})
	}
}

func TestUsageCacheCancellationDoesNotPoisonWaiter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := NewUsageCache()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		started := make(chan struct{})
		cancelled := make(chan error, 1)
		go func() {
			_, err := cache.GetOrFetch(ctx, "key", func(ctx context.Context) (*ProviderUsage, error) { close(started); <-ctx.Done(); return nil, ctx.Err() })
			cancelled <- err
		}()
		<-started
		waiting := make(chan error, 1)
		reading := &ProviderUsage{Provider: "healthy", FetchedAt: time.Now()}
		calls := 0
		go func() {
			got, err := cache.GetOrFetch(t.Context(), "key", func(context.Context) (*ProviderUsage, error) { calls++; return reading, nil })
			if got != reading && err == nil {
				err = errors.New("waiter lost reading")
			}
			waiting <- err
		}()
		cancel()
		if err := <-cancelled; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel cause lost: %v", err)
		}
		if err := <-waiting; err != nil {
			t.Fatalf("cancel poisoned healthy waiter: %v", err)
		}
		if calls != 1 {
			t.Fatalf("healthy retry calls=%d", calls)
		}
	})
}
