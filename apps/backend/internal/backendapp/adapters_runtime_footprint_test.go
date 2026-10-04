package backendapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/agent/executor"
	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
	"github.com/kandev/kandev/internal/agentctl/server/process"
	agentruntime "github.com/kandev/kandev/internal/agentruntime"
	orchestratorexecutor "github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
)

// footprintObserverCapability is the shape the orchestrator asserts its agent
// manager to before observing live runtime footprint
// (internal/orchestrator/runtime_footprint.go). The production agent manager is
// this adapter, so an adapter without that method leaves the assertion false and
// every observation returns early.
type footprintObserverCapability interface {
	SnapshotRuntimeFootprint(context.Context) agentruntime.RuntimeFootprintSnapshot
}

// footprintReaderBackend is a runtime backend that reports host-local process
// footprints, which is what makes a lifecycle manager able to answer an
// observation at all.
type footprintReaderBackend struct {
	name     executor.Name
	readings []agentctl.RuntimeFootprint
	err      error
}

func (b footprintReaderBackend) Name() executor.Name { return b.name }

func (b footprintReaderBackend) HealthCheck(context.Context) error { return nil }

func (b footprintReaderBackend) CreateInstance(
	context.Context, *lifecycle.ExecutorCreateRequest,
) (*lifecycle.ExecutorInstance, error) {
	return nil, errors.New("not implemented in this test backend")
}

func (b footprintReaderBackend) StopInstance(context.Context, *lifecycle.ExecutorInstance, bool) error {
	return nil
}

func (b footprintReaderBackend) RecoverInstances(
	context.Context, []*models.ExecutorRunning,
) ([]*lifecycle.ExecutorInstance, error) {
	return nil, nil
}

func (b footprintReaderBackend) GetInteractiveRunner() *process.InteractiveRunner { return nil }

func (b footprintReaderBackend) RequiresCloneURL() bool { return false }

func (b footprintReaderBackend) ShouldApplyPreferredShell() bool { return false }

func (b footprintReaderBackend) IsAlwaysResumable() bool { return false }

func (b footprintReaderBackend) ReadRuntimeFootprints(context.Context) ([]agentctl.RuntimeFootprint, error) {
	return b.readings, b.err
}

func newFootprintManager(t *testing.T, backend lifecycle.ExecutorBackend) *lifecycle.Manager {
	t.Helper()

	var registry *lifecycle.ExecutorRegistry
	if backend != nil {
		registry = lifecycle.NewExecutorRegistry(newTestLogger())
		registry.Register(backend)
	}
	return lifecycle.NewManager(
		nil, nil, registry, nil, nil, nil,
		lifecycle.ExecutorFallbackDeny, t.TempDir(), newTestLogger(),
	)
}

// TestLifecycleAdapterSatisfiesRuntimeFootprintObserver pins the production
// wiring the orchestrator depends on: the value it holds as its agent manager is
// this adapter, and an adapter that cannot be asserted to the footprint
// observer capability leaves every observation returning early, so the published
// projection stays at its zero value forever.
func TestLifecycleAdapterSatisfiesRuntimeFootprintObserver(t *testing.T) {
	var agentManager orchestratorexecutor.AgentManagerClient = newLifecycleAdapter(
		newFootprintManager(t, nil), nil, newTestLogger(),
	)

	observer, ok := agentManager.(footprintObserverCapability)
	require.True(t, ok,
		"the production agent manager must satisfy the runtime footprint observer capability")
	require.NotNil(t, observer)
}

// TestLifecycleAdapterForwardsRuntimeFootprintSnapshot pins that the adapter
// returns the lifecycle manager's own observation rather than a value of its own
// construction: a reader that saw an incomplete or empty reading here must see
// the manager's reading unchanged, and a reader that saw measured bytes must see
// those same bytes.
func TestLifecycleAdapterForwardsRuntimeFootprintSnapshot(t *testing.T) {
	lastActivity := time.Now().UTC().Add(-30 * time.Second)
	measured := agentctl.RuntimeFootprint{
		InstanceID: "instance-1", SessionID: "session-1", Status: "running",
		Processes: 4, CommittedBytes: 4096, ResidentBytes: 2048, LastActivity: lastActivity,
	}

	t.Run("a measured reading reaches the caller unchanged", func(t *testing.T) {
		mgr := newFootprintManager(t, footprintReaderBackend{
			name:     executor.Name("test-footprint"),
			readings: []agentctl.RuntimeFootprint{measured},
		})
		adapter := newLifecycleAdapter(mgr, nil, newTestLogger())

		snapshot := adapter.SnapshotRuntimeFootprint(context.Background())

		require.True(t, snapshot.Complete, "a successful read must stay complete through the adapter")
		require.Equal(t, 1, snapshot.LiveRuntimes)
		require.Equal(t, 4, snapshot.ProcessCount)
		require.Equal(t, uint64(4096), snapshot.CommittedBytes)
		require.Equal(t, uint64(2048), snapshot.ResidentBytes)
		require.Equal(t, mgr.SnapshotRuntimeFootprint(context.Background()), snapshot,
			"the adapter must forward the manager's observation, not reshape it")
	})

	t.Run("a failed read stays fail-closed through the adapter", func(t *testing.T) {
		mgr := newFootprintManager(t, footprintReaderBackend{
			name: executor.Name("test-footprint"),
			err:  errors.New("control server unreachable"),
		})
		adapter := newLifecycleAdapter(mgr, nil, newTestLogger())

		snapshot := adapter.SnapshotRuntimeFootprint(context.Background())

		require.False(t, snapshot.Complete, "a failed read must not be reported as complete")
		require.Zero(t, snapshot.LiveRuntimes)
		require.Zero(t, snapshot.ProcessCount)
		require.Empty(t, snapshot.Runtimes)
	})
}
