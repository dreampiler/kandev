package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/task/models"
)

type fakeCoordinatorProfiles struct {
	coordinators map[string]bool
	err          error
}

func (f fakeCoordinatorProfiles) IsWorkspaceCoordinatorProfile(_ context.Context, profileID string) (bool, error) {
	return f.coordinators[profileID], f.err
}

// coordinatorRequest is an automatic sessionless launch, as an Office
// coordinator run reaches seam 1, whose own record was queued queuedFor ago.
func coordinatorRequest(c *sessionCeilingController, profileID string, queuedFor time.Duration) admissionRequest {
	return admissionRequest{
		taskID: "parent", origin: launchOriginAutomatic, seam: "startTask",
		agentProfileID: profileID, coordinator: true,
		coordinatorQueuedSince: c.now().Add(-queuedFor),
	}
}

func saturatedController(t *testing.T) (*sessionCeilingController, *fakeAdmittedLister) {
	t.Helper()
	lister := &fakeAdmittedLister{profiles: map[string]string{"s1": "worker", "s2": "worker"}}
	lister.set("s1", "s2")
	c := newTestController(t, 2, lister)
	now := time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	return c, lister
}

func TestCoordinatorAllowanceAdmitsAnAgedCoordinatorIntoASaturatedLane(t *testing.T) {
	c, _ := saturatedController(t)

	d := c.admit(context.Background(), coordinatorRequest(c, "coord", coordinatorAllowanceAge+time.Second))
	require.True(t, d.admitted, "an aged coordinator launch is admitted past a saturated worker lane")
	require.Equal(t, ceilingReasonCoordinatorAllowance, d.reasonCode)
	require.False(t, d.manualOverride)
	require.Equal(t, ceilingClassWorker, d.class)
	require.Equal(t, 3, mustPopulation(t, c), "the admitted coordinator counts in the worker population")
}

func TestCoordinatorAllowanceRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*sessionCeilingController, *fakeAdmittedLister, *admissionRequest)
	}{
		{"not yet aged", func(c *sessionCeilingController, _ *fakeAdmittedLister, req *admissionRequest) {
			req.coordinatorQueuedSince = c.now().Add(-coordinatorAllowanceAge + time.Second)
		}},
		{"no own queued record", func(_ *sessionCeilingController, _ *fakeAdmittedLister, req *admissionRequest) {
			req.coordinatorQueuedSince = time.Time{}
		}},
		{"not a coordinator", func(_ *sessionCeilingController, _ *fakeAdmittedLister, req *admissionRequest) {
			req.coordinator = false
		}},
		{"same profile already counted", func(_ *sessionCeilingController, lister *fakeAdmittedLister, _ *admissionRequest) {
			lister.profiles["s2"] = "coord"
		}},
		{"population unreadable for the profile check", func(c *sessionCeilingController, lister *fakeAdmittedLister, _ *admissionRequest) {
			c.lister = &profileFailingLister{inner: lister}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, lister := saturatedController(t)
			req := coordinatorRequest(c, "coord", coordinatorAllowanceAge+time.Minute)
			tc.mutate(c, lister, &req)

			d := c.admit(context.Background(), req)
			require.False(t, d.admitted, "decision = %+v", d)
			require.Equal(t, ceilingReasonRefused, d.reasonCode)
		})
	}
}

// profileFailingLister answers the admission's population read and then fails,
// so only the allowance's own profile read sees an unreadable population.
type profileFailingLister struct {
	inner *fakeAdmittedLister
	calls int
}

func (p *profileFailingLister) ListAdmittedSessionRefs(ctx context.Context) ([]models.AdmittedSessionRef, error) {
	p.calls++
	if p.calls > 1 {
		return nil, errors.New("population unavailable")
	}
	return p.inner.ListAdmittedSessionRefs(ctx)
}

func TestCoordinatorAllowanceIsBoundedToOneSessionPerProfile(t *testing.T) {
	c, _ := saturatedController(t)
	first := c.admit(context.Background(), coordinatorRequest(c, "coord", time.Hour))
	require.True(t, first.admitted)

	second := c.admit(context.Background(), coordinatorRequest(c, "coord", time.Hour))
	require.False(t, second.admitted, "a second session of the same coordinator profile is refused while the first is reserved")

	other := c.admit(context.Background(), coordinatorRequest(c, "other-coord", time.Hour))
	require.True(t, other.admitted, "another workspace's coordinator has its own bound")
}

func TestCoordinatorAllowanceStillYieldsToHostMemoryPressure(t *testing.T) {
	lister := &fakeAdmittedLister{profiles: map[string]string{}}
	lister.set("s1", "s2")
	c := newMemoryController(t, 2, lister, &fakeMemoryReader{reading: pressuredReading()})

	d := c.admit(context.Background(), coordinatorRequest(c, "coord", time.Hour))
	require.False(t, d.admitted)
	require.Equal(t, ceilingReasonMemoryPressure, d.reasonCode)
}

func TestManualAndFreeLaneAdmissionsAreUnchangedForCoordinators(t *testing.T) {
	c, lister := saturatedController(t)
	manual := coordinatorRequest(c, "coord", 0)
	manual.origin = launchOriginManual
	d := c.admit(context.Background(), manual)
	require.Equal(t, ceilingReasonManualOverride, d.reasonCode)

	lister.set("s1")
	c.reservations = map[string]*ceilingReservation{}
	free := c.admit(context.Background(), coordinatorRequest(c, "coord", 0))
	require.True(t, free.admitted)
	require.Empty(t, free.reasonCode, "a free worker slot admits a coordinator the ordinary way")
}

func coordinatorCeilingTask(id, priority, profileID string, office bool) *models.Task {
	task := orderedCeilingTask(id, priority, 0, "2026-10-14T05:00:00Z")
	task.IsFromOffice = office
	record := task.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	record[models.CeilingLaunchPayloadKey] = map[string]interface{}{metaKeyAgentProfileID: profileID}
	return task
}

func TestCeilingReplayDrainVisitsCoordinatorRecordsFirst(t *testing.T) {
	for _, tc := range []struct {
		name    string
		wired   bool
		office  bool
		visited []string
	}{
		{"coordinator record first", true, true, []string{"coord-task", "worker-task"}},
		{"unwired reader keeps the old order", false, true, []string{"worker-task", "coord-task"}},
		{"a non-Office task is never a coordinator record", true, false, []string{"worker-task", "coord-task"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newServiceWithRealRepo(t)
			repo := &orderedCeilingReplayRepo{sessionExecutorStore: svc.repo, tasks: []*models.Task{
				coordinatorCeilingTask("worker-task", "critical", "worker", false),
				coordinatorCeilingTask("coord-task", "low", "coord", tc.office),
			}}
			svc.repo = repo
			if tc.wired {
				svc.SetCeilingCoordinatorProfiles(fakeCoordinatorProfiles{coordinators: map[string]bool{"coord": true}})
			}
			svc.drainDeferredCeilingLaunches(context.Background())
			require.Equal(t, tc.visited, repo.visited)
		})
	}
}

// A coordinator request never yields a free slot to an ordinary queued record.
func TestCoordinatorRequestDoesNotYieldToAnOrdinaryRecord(t *testing.T) {
	svc, _ := newServiceWithRealRepo(t)
	svc.repo = &orderedCeilingReplayRepo{sessionExecutorStore: svc.repo, tasks: []*models.Task{
		coordinatorCeilingTask("worker-task", "critical", "worker", false),
	}}
	svc.SetCeilingCoordinatorProfiles(fakeCoordinatorProfiles{coordinators: map[string]bool{"coord": true}})

	req := admissionRequest{taskID: "parent", origin: launchOriginAutomatic, agentProfileID: "coord", coordinator: true}
	got := svc.ceilingDeferredLaunchPrecedes(context.Background(), req, ceilingClassWorker)
	require.False(t, got.precedes)
}

func TestCoordinatorProfileReadFailsClosed(t *testing.T) {
	svc, _ := newServiceWithRealRepo(t)
	svc.SetCeilingCoordinatorProfiles(fakeCoordinatorProfiles{
		coordinators: map[string]bool{"coord": true}, err: errors.New("store down"),
	})
	req := svc.withCoordinatorAdmission(context.Background(),
		admissionRequest{taskID: "parent", origin: launchOriginAutomatic, agentProfileID: "coord"})
	require.False(t, req.coordinator)
}

type closedOwnerReminter struct{ calls int }

type closedOwnerErr struct{}

func (closedOwnerErr) Error() string           { return "run is cancelled" }
func (closedOwnerErr) LaunchOwnerClosed() bool { return true }

func (r *closedOwnerReminter) RemintLaunchCredentials(context.Context, string, string, map[string]string) (map[string]string, error) {
	r.calls++
	return nil, fmt.Errorf("wrapped: %w", closedOwnerErr{})
}

// A stored Office start whose run is over is dropped by the replay instead of
// starting an agent with the first payload it was refused with.
func TestCeilingStartReplayDropsWhenItsRunNoLongerWaits(t *testing.T) {
	svc, _ := newServiceWithRealRepo(t)
	reminter := &closedOwnerReminter{}
	svc.SetLaunchCredentialReminter(reminter)

	result := svc.replayCeilingLaunchStart(context.Background(), &models.Task{ID: "parent", IsFromOffice: true},
		map[string]interface{}{"env": map[string]interface{}{"KANDEV_RUN_ID": "run-1"}})
	require.Equal(t, 1, reminter.calls)
	require.Equal(t, ceilingReplayRunClosed, result.outcome)
	require.Equal(t, ceilingDetailLaunchOwnerClosed, result.detail)
}
