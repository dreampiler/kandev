package usage

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUsageCache_RetryAfterSuppressesProvider(t *testing.T) {
	cache := NewUsageCache()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cache.SetClockForTest(func() time.Time { return now })

	var calls int32
	fetchFn := func(_ context.Context) (*ProviderUsage, error) {
		atomic.AddInt32(&calls, 1)
		return nil, &FetchError{
			Provider:   "anthropic",
			Reason:     FailureHTTPStatus,
			Status:     http.StatusTooManyRequests,
			RetryAfter: 120 * time.Second,
		}
	}

	// First call hits provider and returns 429 with Retry-After: 120s
	_, err := cache.GetOrFetch(context.Background(), "test-key", fetchFn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// Advance 60s (still within 120s backoff) -> should NOT hit provider
	now = now.Add(60 * time.Second)
	_, err = cache.GetOrFetch(context.Background(), "test-key", fetchFn)
	if err == nil {
		t.Fatal("expected cached error, got nil")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected still 1 call during backoff, got %d", calls)
	}

	// Advance past 120s (121s) -> backoff expired, should hit provider again
	now = now.Add(61 * time.Second)
	_, err = cache.GetOrFetch(context.Background(), "test-key", fetchFn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls after backoff expired, got %d", calls)
	}
}

func TestUsageCache_ExponentialBackoffWithoutHeader(t *testing.T) {
	cache := NewUsageCache()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cache.SetClockForTest(func() time.Time { return now })

	var calls int32
	fetchFn := func(_ context.Context) (*ProviderUsage, error) {
		atomic.AddInt32(&calls, 1)
		return nil, &FetchError{
			Provider: "anthropic",
			Reason:   FailureHTTPStatus,
			Status:   http.StatusTooManyRequests,
		}
	}

	// Failure 1: 15s backoff
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("call 1: got %d", calls)
	}

	// Advance 10s: still in backoff
	now = now.Add(10 * time.Second)
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("should still be 1 call during 15s backoff, got %d", calls)
	}

	// Advance 6s (total 16s > 15s): retry -> Failure 2: 30s backoff
	now = now.Add(6 * time.Second)
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("call 2: got %d", calls)
	}

	// Advance 25s (< 30s): still in backoff
	now = now.Add(25 * time.Second)
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("should still be 2 calls during 30s backoff, got %d", calls)
	}

	// Advance 6s (total 31s > 30s): retry -> Failure 3: 60s backoff
	now = now.Add(6 * time.Second)
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("call 3: got %d", calls)
	}

	// Advance 55s (< 60s): still in backoff
	now = now.Add(55 * time.Second)
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("should still be 3 calls during 60s backoff, got %d", calls)
	}
}

func TestUsageCache_SuccessResetsBackoff(t *testing.T) {
	cache := NewUsageCache()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cache.SetClockForTest(func() time.Time { return now })

	var shouldSucceed bool
	var calls int32
	fetchFn := func(_ context.Context) (*ProviderUsage, error) {
		atomic.AddInt32(&calls, 1)
		if shouldSucceed {
			return &ProviderUsage{Provider: "anthropic", FetchedAt: now}, nil
		}
		return nil, &FetchError{Provider: "anthropic", Reason: FailureHTTPStatus, Status: 429}
	}

	// Fail twice -> backoff becomes 30s
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	now = now.Add(16 * time.Second)
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}

	// Now succeed after 31s
	now = now.Add(31 * time.Second)
	shouldSucceed = true
	usage, err := cache.GetOrFetch(context.Background(), "key", fetchFn)
	if err != nil || usage == nil {
		t.Fatalf("expected success, got usage=%v, err=%v", usage, err)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}

	// Advance past cacheTTL (5 min): next fetch fails -> should start backoff at 15s again (reset!)
	now = now.Add(6 * time.Minute)
	shouldSucceed = false
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", calls)
	}

	// Advance 16s: if reset worked, backoff was 15s, so at 16s it should attempt retry
	now = now.Add(16 * time.Second)
	_, _ = cache.GetOrFetch(context.Background(), "key", fetchFn)
	if atomic.LoadInt32(&calls) != 5 {
		t.Fatalf("expected 5 calls (failure was reset to 15s), got %d", calls)
	}
}

func TestUsageCache_ServesStaleWithinMaxAge(t *testing.T) {
	cache := NewUsageCache()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cache.SetClockForTest(func() time.Time { return now })

	initialSuccess := &ProviderUsage{Provider: "anthropic", Plan: "pro", FetchedAt: now}
	cache.storeSuccess("key", initialSuccess)

	// Advance 6 minutes (> cacheTTL 5m, so fresh success expired)
	now = now.Add(6 * time.Minute)

	// Fetch fails with 429 Retry-After: 600s (10 min)
	fetchFn := func(_ context.Context) (*ProviderUsage, error) {
		return nil, &FetchError{
			Provider:   "anthropic",
			Reason:     FailureHTTPStatus,
			Status:     429,
			RetryAfter: 600 * time.Second,
		}
	}

	got, err := cache.GetOrFetch(context.Background(), "key", fetchFn)
	// While inside maxStaleUsageAge (30m), serving stale reading instead of error!
	if err != nil {
		t.Fatalf("expected stale success, got err: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil stale usage")
	}
	if !got.Stale {
		t.Error("expected Stale == true")
	}
	if got.Plan != "pro" {
		t.Errorf("Plan = %q, want pro", got.Plan)
	}

	// Advance beyond maxStaleUsageAge (e.g. 31 minutes from initial success: 12:31)
	now = time.Date(2026, 10, 5, 12, 31, 0, 0, time.UTC)
	// Incur another failure with backoff at 12:31
	gotPast, errPast := cache.GetOrFetch(context.Background(), "key", fetchFn)
	// Since 31 min > 30 min, stale is NOT served. Error must be reported.
	if errPast == nil {
		t.Fatal("expected error beyond maxStaleUsageAge, got nil")
	}
	if gotPast != nil {
		t.Fatalf("expected nil usage beyond maxStaleUsageAge, got %v", gotPast)
	}
}

func TestUsageCache_ConcurrentCallersCoalesce(t *testing.T) {
	cache := NewUsageCache()
	var calls int32
	startBlock := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once

	fetchFn := func(ctx context.Context) (*ProviderUsage, error) {
		atomic.AddInt32(&calls, 1)
		once.Do(func() { close(started) })
		<-startBlock
		return &ProviderUsage{Provider: "anthropic", Plan: "team"}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.GetOrFetch(context.Background(), "concurrent-key", fetchFn)
		}()
	}

	<-started
	close(startBlock)
	wg.Wait()

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly 1 fetch call across 5 concurrent callers, got %d", calls)
	}
}
