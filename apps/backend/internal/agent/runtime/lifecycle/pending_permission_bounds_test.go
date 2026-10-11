package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGetBySessionIDContextReturnsDeadlineWhenRegistryHeld(t *testing.T) {
	store := NewExecutionStore()
	store.mu.Lock()
	defer store.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := store.GetBySessionIDContext(ctx, "session-held")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetBySessionIDContext while write-held = %v, want context.DeadlineExceeded", err)
	}
}

func TestGetBySessionIDContextResolvesWhenFree(t *testing.T) {
	store := NewExecutionStore()
	if err := store.Add(&AgentExecution{ID: "exec-1", SessionID: "session-1"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	execution, ok, err := store.GetBySessionIDContext(ctx, "session-1")
	if err != nil || !ok || execution.ID != "exec-1" {
		t.Fatalf("GetBySessionIDContext = (%v, %v, %v), want exec-1, true, nil", execution, ok, err)
	}
}

// TestRemoveReleasesRegistryLockBeforeClearingRuntimeEnvironment is the
// regression test for one session's cancel escalation holding the shared
// registry write lock while clearing per-execution runtime state, which blocked
// another session's pending-permission lookup acquisition.
func TestRemoveReleasesRegistryLockBeforeClearingRuntimeEnvironment(t *testing.T) {
	store := NewExecutionStore()
	removed := &AgentExecution{ID: "exec-removed", SessionID: "session-removed"}
	other := &AgentExecution{ID: "exec-other", SessionID: "session-other"}
	if err := store.Add(removed); err != nil {
		t.Fatalf("Add(removed): %v", err)
	}
	if err := store.Add(other); err != nil {
		t.Fatalf("Add(other): %v", err)
	}
	removed.setRuntimeEnvironment(map[string]string{"HOME": "/tmp/home"})

	// Hold the per-execution environment lock so Remove blocks inside
	// clearRuntimeEnvironment after it has released the registry lock.
	removed.runtimeEnvMu.Lock()
	removeDone := make(chan struct{})
	go func() {
		store.Remove("exec-removed")
		close(removeDone)
	}()

	// The registry write lock must be released (and the entry removed) while
	// Remove is still blocked on runtimeEnvMu.
	deadline := time.Now().Add(2 * time.Second)
	released := false
	for time.Now().Before(deadline) {
		if store.mu.TryRLock() {
			_, present := store.executions["exec-removed"]
			store.mu.RUnlock()
			if !present {
				released = true
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if !released {
		removed.runtimeEnvMu.Unlock()
		<-removeDone
		t.Fatal("Remove held the registry write lock while clearing runtime environment")
	}

	// An unrelated session still resolves while Remove is blocked.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lookup, ok, err := store.GetBySessionIDContext(ctx, "session-other")
	if err != nil || !ok || lookup.ID != "exec-other" {
		removed.runtimeEnvMu.Unlock()
		<-removeDone
		t.Fatalf("concurrent lookup while Remove blocked = (%v, %v, %v), want exec-other, true, nil", lookup, ok, err)
	}

	removed.runtimeEnvMu.Unlock()
	select {
	case <-removeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Remove did not finish after runtimeEnvMu was released")
	}

	if env := removed.RuntimeEnvironment(); env != nil {
		t.Fatalf("runtime environment after Remove = %v, want nil", env)
	}
	if _, ok := store.GetBySessionID("session-removed"); ok {
		t.Fatal("removed session still resolvable in registry")
	}
}
