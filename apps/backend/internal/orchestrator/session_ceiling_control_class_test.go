package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const controlProfileFixtureID = "profile-control"

func newClassController(
	workerCeiling, controlCeiling int, controlProfiles []string, lister *fakeAdmittedLister,
) *sessionCeilingController {
	controller := newSessionCeilingController(workerCeiling, lister, zap.NewNop())
	controller.controlCeiling = controlCeiling
	controller.controlProfiles = controlProfileSet(controlProfiles)
	return controller
}

func controlAdmission(sessionID string) admissionRequest {
	return admissionRequest{
		taskID: "task-" + sessionID, sessionID: sessionID,
		origin: launchOriginAutomatic, seam: "test",
		agentProfileID: controlProfileFixtureID,
	}
}

func workerAdmission(sessionID string) admissionRequest {
	req := controlAdmission(sessionID)
	req.agentProfileID = "profile-worker"
	return req
}

// TestControlLaneAdmitsWhileWorkerLaneIsSaturated is the feature's purpose: a
// full worker ceiling must not refuse a control launch, and the control launch
// must not consume a worker slot.
func TestControlLaneAdmitsWhileWorkerLaneIsSaturated(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.set("running-worker")
	controller := newClassController(1, 2, []string{controlProfileFixtureID}, lister)

	decision := controller.admit(ctx, controlAdmission("control-launch"))

	require.True(t, decision.admitted)
	require.Equal(t, ceilingClassControl, decision.class)
	require.Equal(t, 2, decision.classCeiling)
	require.Equal(t, 0, decision.classPopulation)
	require.Equal(t, 1, decision.population, "the instance total still spans both lanes")
}

// TestWorkerLaneRefusalIsUnchangedWhenNoControlLaneExists pins the
// no-control-lane case to the historical behavior and reason code.
func TestWorkerLaneRefusalIsUnchangedWhenNoControlLaneExists(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.set("running-worker")
	controller := newClassController(1, 0, nil, lister)

	decision := controller.admit(ctx, workerAdmission("worker-launch"))

	require.False(t, decision.admitted)
	require.Equal(t, ceilingReasonRefused, decision.reasonCode)
	require.Equal(t, ceilingClassWorker, decision.class)
}

// TestControlLaneRefusalUsesItsOwnReasonCode keeps the two lanes distinguishable
// in a log line or a card without reading the counts.
func TestControlLaneRefusalUsesItsOwnReasonCode(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{
		"control-a": controlProfileFixtureID,
		"control-b": controlProfileFixtureID,
	}, "control-a", "control-b")
	controller := newClassController(8, 2, []string{controlProfileFixtureID}, lister)

	decision := controller.admit(ctx, controlAdmission("control-c"))

	require.False(t, decision.admitted)
	require.Equal(t, ceilingReasonControlRefused, decision.reasonCode)
	require.Equal(t, 2, decision.classPopulation)
	require.Equal(t, 2, decision.population)
}

// TestUnknownProfileFallsIntoTheWorkerLane is the fail-closed direction: a
// session whose profile cannot be resolved must never take reserved control
// capacity.
func TestUnknownProfileFallsIntoTheWorkerLane(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{
		"worker-a": "profile-worker",
		"worker-b": "profile-worker",
	}, "worker-a", "worker-b")
	controller := newClassController(2, 2, []string{controlProfileFixtureID}, lister)

	unresolved := controlAdmission("no-profile-session")
	unresolved.agentProfileID = ""
	decision := controller.admit(ctx, unresolved)

	require.False(t, decision.admitted)
	require.Equal(t, ceilingClassWorker, decision.class)
	require.Equal(t, ceilingReasonRefused, decision.reasonCode)
}

// TestClassPopulationCountsPersistedRowsByStoredProfile proves the controller
// classifies persisted rows by the profile stored on the row, not by any value
// a launch caller supplied.
func TestClassPopulationCountsPersistedRowsByStoredProfile(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{
		"worker-a":  "profile-worker",
		"control-a": controlProfileFixtureID,
		"bare-a":    "",
	}, "worker-a", "control-a", "bare-a")
	controller := newClassController(8, 2, []string{controlProfileFixtureID}, lister)

	counted, err := controller.countedRowsLocked(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, controller.classPopulationLocked(counted, ceilingClassWorker))
	require.Equal(t, 1, controller.classPopulationLocked(counted, ceilingClassControl))
	require.Equal(t, 3, controller.populationLocked(counted))
}

// TestLaunchScopedReservationCountsInItsOwnLane covers the seam-1 window, where
// the session does not exist yet: the reservation has to hold the lane it was
// admitted into or a concurrent launch could take the same control slot.
func TestLaunchScopedReservationCountsInItsOwnLane(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	controller := newClassController(8, 2, []string{controlProfileFixtureID}, lister)

	launchScoped := controlAdmission("")
	require.True(t, controller.admit(ctx, launchScoped).admitted)

	counted, err := controller.countedRowsLocked(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, controller.classPopulationLocked(counted, ceilingClassControl))
	require.Equal(t, 0, controller.classPopulationLocked(counted, ceilingClassWorker))
}

// TestRebindKeepsTheReservationLane pins the rebind path: moving a launch-scoped
// reservation onto the created session must not change which lane it occupies.
func TestRebindKeepsTheReservationLane(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	controller := newClassController(8, 2, []string{controlProfileFixtureID}, lister)

	decision := controller.admit(ctx, controlAdmission(""))
	require.True(t, decision.admitted)
	require.True(t, controller.rebind(decision.reservationKey, "control-created"))

	counted, err := controller.countedRowsLocked(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, controller.classPopulationLocked(counted, ceilingClassControl))
}

// TestHandOffKeepsTheCountedRowsLane covers the dynamic relaunch seam: a
// replacement process for a session already counted in the control lane must not
// be re-readmitted against the worker ceiling.
func TestHandOffKeepsTheCountedRowsLane(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{"control-a": controlProfileFixtureID}, "control-a")
	controller := newClassController(1, 2, []string{controlProfileFixtureID}, lister)

	// The worker lane is saturated, so an ordinary worker admission would be
	// refused. The hand-off is neither.
	decision := controller.handOffOrAdmit(ctx, workerAdmission("control-a"))

	require.True(t, decision.admitted)
	require.True(t, decision.handedOff)
	require.Equal(t, ceilingClassControl, decision.class)
	require.Equal(t, 2, decision.classCeiling)
}

// TestRestartRebuildsClassCountsFromPersistedRowsOnly pins the restart contract:
// a fresh controller with no in-process reservations still counts each lane from
// durable rows alone.
func TestRestartRebuildsClassCountsFromPersistedRowsOnly(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{
		"worker-a":  "profile-worker",
		"worker-b":  "profile-worker",
		"worker-c":  "profile-worker",
		"control-a": controlProfileFixtureID,
	}, "worker-a", "worker-b", "worker-c", "control-a")

	restarted := newClassController(3, 1, []string{controlProfileFixtureID}, lister)
	require.Empty(t, restarted.reservations, "a restarted controller holds no in-process reservations")

	require.False(t, restarted.admit(ctx, workerAdmission("worker-d")).admitted)
	require.False(t, restarted.admit(ctx, controlAdmission("control-b")).admitted)

	observation, err := restarted.observation(ctx)
	require.NoError(t, err)
	require.True(t, observation.Known)
	require.Equal(t, 3, observation.Limit)
	require.Equal(t, 4, observation.InUse)
	require.True(t, observation.ControlConfigured)
	require.Equal(t, 1, observation.ControlLimit)
	require.Equal(t, 1, observation.ControlInUse)
}

// TestManualOverrideStillAdmitsAcrossBothLanes keeps the pre-existing manual
// escape hatch intact in each lane.
func TestManualOverrideStillAdmitsAcrossBothLanes(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{
		"worker-a": "profile-worker", "control-a": controlProfileFixtureID,
	}, "worker-a", "control-a")
	controller := newClassController(1, 1, []string{controlProfileFixtureID}, lister)

	worker := workerAdmission("worker-manual")
	worker.origin = launchOriginManual
	workerDecision := controller.admit(ctx, worker)
	require.True(t, workerDecision.admitted)
	require.True(t, workerDecision.manualOverride)
	require.Equal(t, ceilingReasonManualOverride, workerDecision.reasonCode)

	control := controlAdmission("control-manual")
	control.origin = launchOriginManual
	controlDecision := controller.admit(ctx, control)
	require.True(t, controlDecision.admitted)
	require.True(t, controlDecision.manualOverride)
}

// TestSetCapacitySwapNeverTouchesRunningSessions pins that changing capacity is a
// forward-only admission change: reservations and the rows they stand for stay
// where they are.
func TestSetCapacitySwapNeverTouchesRunningSessions(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{"control-a": controlProfileFixtureID}, "control-a")
	controller := newClassController(8, 2, []string{controlProfileFixtureID}, lister)

	changed := controller.setCapacity(SessionCeilingCapacity{
		WorkerCeiling: 8, ControlCeiling: 2, ControlProfileIDs: []string{controlProfileFixtureID},
	})
	require.False(t, changed, "applying the same capacity is not a change")
	require.Equal(t, 2, controller.classCeilingLocked(ceilingClassControl))

	changed = controller.setCapacity(SessionCeilingCapacity{WorkerCeiling: 8})
	require.True(t, changed)
	require.Nil(t, controller.controlProfiles)

	observation, err := controller.observation(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, observation.InUse, "the already-running control session is untouched")
	require.False(t, observation.ControlConfigured)
}

// TestControlLaneWithoutProfilesIsInert guards the half-configured shape: a
// control ceiling with no profile set must not silently admit an unbounded lane.
func TestControlLaneWithoutProfilesIsInert(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.set("worker-a")
	controller := newClassController(1, 5, nil, lister)

	decision := controller.admit(ctx, controlAdmission("control-a"))

	require.False(t, decision.admitted)
	require.Equal(t, ceilingClassWorker, decision.class)
}

// TestClassifiedSessionIsCountedOnceAcrossBothLanes keeps the union contract: a
// session holding both a counted row and a reservation consumes one unit.
func TestClassifiedSessionIsCountedOnceAcrossBothLanes(t *testing.T) {
	ctx := context.Background()
	lister := &fakeAdmittedLister{}
	lister.setWithProfiles(map[string]string{"control-a": controlProfileFixtureID}, "control-a")
	controller := newClassController(8, 2, []string{controlProfileFixtureID}, lister)

	require.True(t, controller.handOffOrAdmit(ctx, controlAdmission("control-a")).admitted)

	counted, err := controller.countedRowsLocked(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, controller.classPopulationLocked(counted, ceilingClassControl))
	require.Equal(t, 1, controller.populationLocked(counted))
}

// TestAdmittedSessionRefListIsEmptyWithoutARepository pins the unbound
// controller's degraded posture for the class-aware lister: with no repository
// every session is a worker, and the control lane can still be measured from
// reservations alone.
func TestAdmittedSessionRefListIsEmptyWithoutARepository(t *testing.T) {
	ctx := context.Background()
	controller := newSessionCeilingController(4, nil, zap.NewNop())
	controller.controlCeiling = 2
	controller.controlProfiles = controlProfileSet([]string{controlProfileFixtureID})

	refs, err := controller.countedRowsLocked(ctx)
	require.NoError(t, err)
	require.Empty(t, refs)
	require.Equal(t, 0, controller.populationLocked(refs))
}
