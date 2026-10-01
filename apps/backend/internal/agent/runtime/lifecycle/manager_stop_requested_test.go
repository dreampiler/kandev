package lifecycle

import (
	"context"
	"errors"
	"testing"

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
