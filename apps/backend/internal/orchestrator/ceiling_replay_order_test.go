package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

type orderedCeilingReplayRepo struct {
	sessionExecutorStore
	tasks   []*models.Task
	visited []string
}

func (r *orderedCeilingReplayRepo) ListTasksWithCeilingDeferred(context.Context) ([]*models.Task, error) {
	return r.tasks, nil
}

func (r *orderedCeilingReplayRepo) GetTask(_ context.Context, id string) (*models.Task, error) {
	r.visited = append(r.visited, id)
	// A removed task is skipped after its replay attempt reaches the read.
	return nil, nil
}

func TestCeilingReplayOrderDrivesDrain(t *testing.T) {
	svc, _ := newServiceWithRealRepo(t)
	repo := &orderedCeilingReplayRepo{sessionExecutorStore: svc.repo, tasks: []*models.Task{
		orderedCeilingTask("a", "low", 0, "2026-09-12T08:30:00Z"),
		orderedCeilingTask("z", "high", 10, "2026-09-12T09:30:00Z"),
	}}
	svc.repo = repo
	svc.drainDeferredCeilingLaunches(context.Background())
	require.Equal(t, []string{"z", "a"}, repo.visited)
}

func orderedCeilingTask(id, priority string, position int, queuedAt string) *models.Task {
	return &models.Task{ID: id, Priority: priority, Position: position, Metadata: map[string]interface{}{models.MetaKeyDeferredLaunch: map[string]interface{}{
		models.CeilingDeferredKey: true, models.CeilingLaunchKindKey: string(models.CeilingLaunchStart), models.CeilingQueuedAtKey: queuedAt,
	}}}
}

func TestCeilingReplayOrder(t *testing.T) {
	const earlier = "2026-09-12T08:30:00Z"
	const later = "2026-09-12T09:30:00Z"
	for _, tc := range []struct {
		name        string
		left, right *models.Task
	}{
		{"priority before position", orderedCeilingTask("z", "critical", 10, later), orderedCeilingTask("a", "high", 0, earlier)},
		{"high before medium", orderedCeilingTask("z", "high", 0, earlier), orderedCeilingTask("a", "medium", 0, earlier)},
		{"medium before low", orderedCeilingTask("z", "medium", 0, earlier), orderedCeilingTask("a", "low", 0, earlier)},
		{"low before unknown", orderedCeilingTask("z", "low", 0, earlier), orderedCeilingTask("a", "", 0, earlier)},
		{"position before time", orderedCeilingTask("z", "high", 0, later), orderedCeilingTask("a", "high", 1, earlier)},
		{"deferral time before id", orderedCeilingTask("z", "high", 0, earlier), orderedCeilingTask("a", "high", 0, later)},
		{"id tie breaker", orderedCeilingTask("a", "high", 0, earlier), orderedCeilingTask("z", "high", 0, earlier)},
		{"missing time last", orderedCeilingTask("z", "high", 0, earlier), orderedCeilingTask("a", "high", 0, "")},
		{"invalid time last", orderedCeilingTask("z", "high", 0, earlier), orderedCeilingTask("a", "high", 0, "invalid")},
		{"missing record last", orderedCeilingTask("z", "high", 0, earlier), &models.Task{ID: "a", Priority: "high"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, ceilingReplayLess(tc.left, tc.right))
			require.False(t, ceilingReplayLess(tc.right, tc.left))
			require.False(t, ceilingReplayLess(tc.left, tc.left))
		})
	}
}

func TestCeilingReplayOrderIgnoresUnrelatedWrites(t *testing.T) {
	left := orderedCeilingTask("z", "high", 0, "2026-09-12T08:30:00Z")
	right := orderedCeilingTask("a", "high", 0, "2026-09-12T09:30:00Z")
	left.UpdatedAt = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	left.Title = "A later update"
	record := left.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	record[models.CeilingPopulationAtRefusalKey] = 99
	record[models.CeilingValueAtRefusalKey] = 100
	require.True(t, ceilingReplayLess(left, right))
}
