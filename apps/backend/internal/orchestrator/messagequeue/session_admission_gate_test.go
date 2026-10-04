package messagequeue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// A queue operation issued while another admitted callback is still running must
// fail with its own context instead of waiting for that callback indefinitely.
func TestWithSessionAdmissionHonorsCallerContextWhileSessionBusy(t *testing.T) {
	svc := &Service{admissions: map[string]*sessionAdmission{}}
	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		_ = svc.WithSessionAdmission(context.Background(), "session-busy", func(context.Context) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := svc.WithSessionAdmission(ctx, "session-busy", func(context.Context) error {
		t.Fatal("admitted while the session admission was held")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the caller deadline, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("admission wait was not bounded by the caller context: %s", elapsed)
	}

	close(release)
	if err := svc.WithSessionAdmission(context.Background(), "session-busy", func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("admission did not recover after the holder finished: %v", err)
	}
}

// The task-wide purge barrier must not turn into an unbounded wait either.
func TestWithSessionAdmissionHonorsCallerContextDuringTaskPurge(t *testing.T) {
	svc := &Service{admissions: map[string]*sessionAdmission{}}
	svc.lifecycleMu.Lock()
	purged := make(chan struct{})
	go func() {
		defer close(purged)
		defer svc.lifecycleMu.Unlock()
		time.Sleep(80 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := svc.WithSessionAdmission(ctx, "session-purge", func(context.Context) error {
		t.Fatal("admitted while a task purge held the lifecycle barrier")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the caller deadline during purge, got %v", err)
	}
	<-purged
	if err := svc.WithSessionAdmission(context.Background(), "session-purge", func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("admission did not recover after the purge: %v", err)
	}
}

// A caller that gives up must not leave a queued waiter behind that later
// acquires the session and releases it into the wrong callback.
func TestWithSessionAdmissionAbandonedWaiterDoesNotStealAdmission(t *testing.T) {
	svc := &Service{admissions: map[string]*sessionAdmission{}}
	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		_ = svc.WithSessionAdmission(context.Background(), "session-abandoned", func(context.Context) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	canceled, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		finished <- svc.WithSessionAdmission(canceled, "session-abandoned", func(context.Context) error {
			return errors.New("abandoned callback ran")
		})
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	close(release)
	time.Sleep(20 * time.Millisecond)

	var mu sync.Mutex
	admitted := 0
	if err := svc.WithSessionAdmission(context.Background(), "session-abandoned", func(context.Context) error {
		mu.Lock()
		admitted++
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("admission after abandonment failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if admitted != 1 {
		t.Fatalf("expected exactly one admission after abandonment, got %d", admitted)
	}
}

// WebSocket-dispatched queue actions run under the hub's lifetime context,
// which carries no deadline. An operation issued there must still return the
// retryable admission-timeout sentinel instead of hanging, and admission must
// recover once the stuck holder releases.
func TestWithSessionAdmissionBoundsCallerWithoutDeadline(t *testing.T) {
	previous := admissionWaitTimeout
	admissionWaitTimeout = 200 * time.Millisecond
	t.Cleanup(func() { admissionWaitTimeout = previous })

	svc := &Service{admissions: map[string]*sessionAdmission{}}
	// No deadline, like Client.dispatchContext.
	hubCtx := context.Background()
	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		_ = svc.WithSessionAdmission(hubCtx, "session-no-deadline", func(context.Context) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	err := svc.WithSessionAdmission(hubCtx, "session-no-deadline", func(context.Context) error {
		t.Fatal("admitted while the session admission was held")
		return nil
	})
	if !errors.Is(err, ErrSessionAdmissionTimeout) {
		t.Fatalf("expected the admission timeout sentinel for a deadline-free caller, got %v", err)
	}

	close(release)
	recovered := make(chan error, 1)
	go func() {
		recovered <- svc.WithSessionAdmission(hubCtx, "session-no-deadline", func(context.Context) error { return nil })
	}()
	select {
	case err := <-recovered:
		if err != nil {
			t.Fatalf("admission did not recover after the stuck holder released: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission stayed blocked after the stuck holder released")
	}
}

// A caller that cancels must still see its own cancellation rather than the
// admission-timeout sentinel.
func TestWithSessionAdmissionPreservesCallerCancellation(t *testing.T) {
	svc := &Service{admissions: map[string]*sessionAdmission{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := svc.WithSessionAdmission(ctx, "session-cancelled", func(context.Context) error {
		t.Fatal("admitted with an already-cancelled context")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the caller's cancellation, got %v", err)
	}
	if errors.Is(err, ErrSessionAdmissionTimeout) {
		t.Fatalf("caller cancellation must not be reported as an admission timeout: %v", err)
	}
}

// Waiters are served first-in-first-out, so a repeated acquirer cannot starve a
// queue operation that arrived earlier.
func TestWithSessionAdmissionServesWaitersInArrivalOrder(t *testing.T) {
	svc := &Service{admissions: map[string]*sessionAdmission{}}
	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		_ = svc.WithSessionAdmission(context.Background(), "session-order", func(context.Context) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	const waiters = 4
	var wg sync.WaitGroup
	order := make(chan int, waiters)
	for index := 0; index < waiters; index++ {
		wg.Add(1)
		go func(position int) {
			defer wg.Done()
			_ = svc.WithSessionAdmission(context.Background(), "session-order", func(context.Context) error {
				order <- position
				return nil
			})
		}(index)
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(order)

	observed := make([]int, 0, waiters)
	for position := range order {
		observed = append(observed, position)
	}
	for index, position := range observed {
		if position != index {
			t.Fatalf("expected FIFO admission order, got %v", observed)
		}
	}
}
