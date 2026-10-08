package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
)

// TestCeilingRetryDrainIsolatesABlockedReplay pins fix A: a replay wedged on a
// blocking read cannot hold the pass's remaining tasks past the per-task
// budget. The pass reaches the later record and completes while the head is
// still wedged; the head's wedged goroutine keeps its record queued for a
// later pass instead of dropping it.
func TestCeilingRetryDrainIsolatesABlockedReplay(t *testing.T) {
	svc, _ := newServiceWithRealRepo(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var secondVisited atomic.Int32
	repo := &blockingCeilingReplayRepo{
		sessionExecutorStore: svc.repo,
		tasks: []*models.Task{
			orderedCeilingTask("head", "high", 0, "2026-09-12T08:30:00Z"),
			orderedCeilingTask("tail", "high", 1, "2026-09-12T09:30:00Z"),
		},
		blockedID:  "head",
		entered:    entered,
		release:    release,
		secondSeen: &secondVisited,
		secondID:   "tail",
	}
	svc.repo = repo
	svc.ceilingReplayTaskTimeout = 50 * time.Millisecond

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.drainDeferredCeilingLaunches(context.Background())
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("head replay never started")
	}
	// The tail must be reached while the head is still wedged: release stays
	// open until cleanup, so a visit to the tail proves the pass moved on.
	require.Eventually(t, func() bool { return secondVisited.Load() == 1 },
		5*time.Second, time.Millisecond, "wedged head blocked the tail")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pass did not complete while the head was wedged")
	}
}

type blockingCeilingReplayRepo struct {
	sessionExecutorStore
	tasks      []*models.Task
	blockedID  string
	secondID   string
	entered    chan struct{}
	release    chan struct{}
	secondSeen *atomic.Int32
}

func (r *blockingCeilingReplayRepo) ListTasksWithCeilingDeferred(context.Context) ([]*models.Task, error) {
	return r.tasks, nil
}

func (r *blockingCeilingReplayRepo) GetTask(ctx context.Context, id string) (*models.Task, error) {
	if id == r.secondID {
		r.secondSeen.Add(1)
	}
	if id == r.blockedID {
		select {
		case <-r.entered:
		default:
			close(r.entered)
		}
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	}
	return r.sessionExecutorStore.GetTask(ctx, id)
}
