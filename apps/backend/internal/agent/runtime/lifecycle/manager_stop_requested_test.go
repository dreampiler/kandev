package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/executor"
	"github.com/kandev/kandev/internal/agent/runtime/activity"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
)

// A stop request that fails before the runtime settles must not leave the
// intentional-stop marker armed: a still-live execution would then ignore every
// later stream disconnect, leaving its active prompt without reattachment or
// failure settlement.
func TestStopAgentWithReasonFailedActivityReleasesStopMarker(t *testing.T) {
	h := newWorkspaceEventsHarness(t)
	h.mgr.SetActivityCoordinator(activity.NewCoordinator(activity.Options{}))
	h.exec.promptDoneCh = make(chan PromptCompletionSignal, 1)
	h.exec.Status = v1.AgentStatusRunning
	generation, err := h.mgr.BeginPrompt(h.exec.ID)
	require.NoError(t, err)
	h.mgr.executionStore.MarkPromptDispatched(h.exec.ID, generation)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	stopErr := h.mgr.StopAgentWithReason(canceledCtx, h.exec.ID, StopReasonTaskDeleted, true)
	require.ErrorIs(t, stopErr, context.Canceled)
	_, retained := h.mgr.executionStore.Get(h.exec.ID)
	require.True(t, retained, "a failed stop must retain the execution for retry")
	require.False(t, h.exec.stopRequested.Load(), "a failed stop must release the stop marker while the execution stays live")

	h.mgr.handleStreamDisconnectWithStartupGeneration(h.exec, errors.New("stream lost after failed stop"), generation, h.exec.startupAttemptSnapshot())
	select {
	case signal := <-h.exec.promptDoneCh:
		require.True(t, signal.IsError)
		require.Equal(t, generation, signal.PromptGeneration)
	default:
		t.Fatal("disconnect after failed stop left the active prompt unsettled")
	}
}

func TestStopAgentWithReasonFailedRuntimeStopReleasesStopMarker(t *testing.T) {
	log := newTestRegistryLogger()
	execRegistry := NewExecutorRegistry(log)
	backend := &retryableStopBackend{
		MockExecutor: MockExecutor{name: executor.NameStandalone},
		stopErr:      errors.New("runtime stop failed"),
	}
	execRegistry.Register(backend)
	mgr := NewManager(newTestRegistry(), &MockEventBus{}, execRegistry, nil, nil, nil, ExecutorFallbackWarn, "", log)
	cleanupManagerStopCh(t, mgr)
	execution := createTestExecution("exec-retryable-stop", "task-retryable-stop", "session-retryable-stop")
	execution.RuntimeName = executor.NameStandalone
	require.NoError(t, mgr.executionStore.Add(execution))
	generation, err := mgr.BeginPrompt(execution.ID)
	require.NoError(t, err)
	mgr.executionStore.MarkPromptDispatched(execution.ID, generation)

	require.ErrorIs(t, mgr.StopAgentWithReason(context.Background(), "exec-retryable-stop", "idle cleanup", false), backend.stopErr)
	_, retained := mgr.executionStore.Get("exec-retryable-stop")
	require.True(t, retained, "a failed runtime stop must retain the execution for retry")
	require.False(t, execution.stopRequested.Load(), "a failed runtime stop must release the stop marker")

	mgr.handleStreamDisconnectWithStartupGeneration(execution, errors.New("stream lost after failed runtime stop"), generation, execution.startupAttemptSnapshot())
	select {
	case signal := <-execution.promptDoneCh:
		require.True(t, signal.IsError)
		require.Equal(t, generation, signal.PromptGeneration)
	default:
		t.Fatal("disconnect after failed runtime stop left the active prompt unsettled")
	}

	// A later successful stop keeps the intentional-stop marker armed.
	backend.stopErr = nil
	require.NoError(t, mgr.StopAgentWithReason(context.Background(), "exec-retryable-stop", "idle cleanup retry", false))
	_, exists := mgr.executionStore.Get("exec-retryable-stop")
	require.False(t, exists)
	require.True(t, execution.stopRequested.Load(), "a completed stop keeps the stop marker armed")
}

// overlappingStopBackend records the stop marker each StopInstance observes.
// The first call signals entry, blocks until released, then fails, so the
// second stop starts while the first still holds the stop lock.
type overlappingStopBackend struct {
	MockExecutor
	execution *AgentExecution
	mu        sync.Mutex
	samples   []bool
	entered   chan struct{}
	release   chan struct{}
	firstErr  error
	secondErr error
}

func (b *overlappingStopBackend) StopInstance(
	ctx context.Context,
	instance *ExecutorInstance,
	force bool,
) error {
	b.mu.Lock()
	index := len(b.samples)
	b.samples = append(b.samples, b.execution.stopRequested.Load())
	b.mu.Unlock()
	if index == 0 {
		b.entered <- struct{}{}
		<-b.release
		return b.firstErr
	}
	return b.secondErr
}

// A stop waiting on the stop lock must arm the marker after it acquires the
// lock: an earlier failing stop disarms the marker while the waiter is still
// queued, and the waiter must not tear the stream down unprotected.
func TestStopAgentWithReasonOverlappingStopsRearmUnderStopLock(t *testing.T) {
	log := newTestRegistryLogger()
	execRegistry := NewExecutorRegistry(log)
	backend := &overlappingStopBackend{
		MockExecutor: MockExecutor{name: executor.NameStandalone},
		entered:      make(chan struct{}, 1),
		release:      make(chan struct{}),
		firstErr:     errors.New("first stop failed"),
		secondErr:    errors.New("second stop failed"),
	}
	execRegistry.Register(backend)
	mgr := NewManager(newTestRegistry(), &MockEventBus{}, execRegistry, nil, nil, nil, ExecutorFallbackWarn, "", log)
	cleanupManagerStopCh(t, mgr)
	execution := createTestExecution("exec-overlap-stop", "task-overlap-stop", "session-overlap-stop")
	execution.RuntimeName = executor.NameStandalone
	backend.execution = execution
	require.NoError(t, mgr.executionStore.Add(execution))

	firstDone := make(chan error, 1)
	go func() { firstDone <- mgr.StopAgentWithReason(context.Background(), execution.ID, "first stop", false) }()
	<-backend.entered
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- mgr.StopAgentWithReason(context.Background(), execution.ID, "second stop", false)
	}()
	// Wait until the queued stop has passed its own arming point so the first
	// stop's disarm cannot precede it; with arming serialized under the stop
	// lock the waiter stays unarmed until it owns the lock, so this wait is
	// bounded and only used as a scheduling barrier for the buggy shape.
	require.Eventually(t, func() bool { return execution.stopRequested.Load() },
		500*time.Millisecond, 5*time.Millisecond, "the queued stop never reached its arming point")
	close(backend.release)
	require.ErrorIs(t, <-firstDone, backend.firstErr)
	require.ErrorIs(t, <-secondDone, backend.secondErr)

	backend.mu.Lock()
	defer backend.mu.Unlock()
	require.Len(t, backend.samples, 2)
	require.True(t, backend.samples[0], "the in-flight stop observes its own marker")
	require.True(t, backend.samples[1], "a stop waiting on the lock must re-arm after the failing stop disarms")
	require.False(t, execution.stopRequested.Load(), "both failed stops must leave the live execution reattachable")
}

// A cleanup or persistence failure after the runtime stop succeeded must not
// reopen the execution: the runtime is terminal even though the row is still
// registered, so reattachment must stay suppressed.
func TestStopAgentWithReasonPostRuntimeStopFailureKeepsStopMarker(t *testing.T) {
	log := newTestRegistryLogger()
	execRegistry := NewExecutorRegistry(log)
	execRegistry.Register(&MockExecutor{name: executor.NameKubernetes})
	mgr := NewManager(newTestRegistry(), &MockEventBus{}, execRegistry, nil, nil, nil, ExecutorFallbackWarn, "", log)
	cleanupManagerStopCh(t, mgr)
	execution := createTestExecution("exec-post-stop-fail", "task-post-stop-fail", "session-post-stop-fail")
	execution.RuntimeName = executor.NameKubernetes
	execution.setMetadataValue(MetadataKeyAuthTokenSecret, "secret-1")
	require.NoError(t, mgr.executionStore.Add(execution))

	err := mgr.StopAgentWithReason(context.Background(), execution.ID, StopReasonTaskDeleted, true)
	require.ErrorContains(t, err, "delete runtime secrets")
	_, retained := mgr.executionStore.Get(execution.ID)
	require.True(t, retained, "the execution row is retained for cleanup retry")
	require.True(t, execution.stopRequested.Load(), "a post-runtime-stop failure must keep the terminal runtime closed for reattachment")
}
