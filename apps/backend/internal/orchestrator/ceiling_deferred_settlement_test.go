package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
)

type recordedCeilingReplay struct {
	taskID, causingRunID, sessionID, turnID string
}

type recordedCeilingDrop struct {
	taskID, causingRunID, reasonCode, detail string
}

type recordingCeilingSettler struct {
	mu       sync.Mutex
	replayed []recordedCeilingReplay
	dropped  []recordedCeilingDrop
}

func (r *recordingCeilingSettler) CeilingDeferredLaunchReplayed(
	_ context.Context, taskID, causingRunID, sessionID, turnID string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replayed = append(r.replayed, recordedCeilingReplay{taskID, causingRunID, sessionID, turnID})
	return nil
}

func (r *recordingCeilingSettler) CeilingDeferredLaunchDropped(
	_ context.Context, taskID, causingRunID, reasonCode, detail string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropped = append(r.dropped, recordedCeilingDrop{taskID, causingRunID, reasonCode, detail})
	return nil
}

func TestSeam1StartPayloadCarriesCausingRunID(t *testing.T) {
	payload := seam1StartPayload(
		"profile-1", "exec-1", "exec-profile-1", "high", "prompt", "step-1",
		false, false, nil, startTaskOptions{CausingRunID: "office-run-1"},
	)
	require.Equal(t, "office-run-1", payload[ceilingPayloadCausingRunIDKey])

	empty := seam1StartPayload(
		"profile-1", "exec-1", "exec-profile-1", "high", "prompt", "step-1",
		false, false, nil, startTaskOptions{},
	)
	require.NotContains(t, empty, ceilingPayloadCausingRunIDKey)
}

func TestCeilingReplayRestoresCausingRunIDAndSettlesIt(t *testing.T) {
	ctx := context.Background()
	f := newLaunchFailureReplayFixture(t, "replay-causing-run", errors.New("unused"))
	f.svc.SetTurnService(&repoBackedTurnService{repo: f.repo})

	// The fixture's stored deferral predates causing-run persistence; add the
	// identity a real Office start would have recorded.
	stored, err := models.ReadCeilingDeferral(deferredLaunchOf(t, f.svc, f.taskID))
	require.NoError(t, err)
	stored.Payload[ceilingPayloadCausingRunIDKey] = "office-run-1"
	_, prior, err := f.repo.GetTaskDeferredLaunch(ctx, f.taskID)
	require.NoError(t, err)
	wrote, lostCompare, err := f.repo.SetTaskDeferredLaunchIfUnchanged(
		ctx, f.taskID, prior, models.CeilingRecordKeys(stored),
	)
	require.NoError(t, err)
	require.True(t, wrote)
	require.False(t, lostCompare)

	settler := &recordingCeilingSettler{}
	f.svc.SetCeilingDeferredLaunchSettler(settler)

	var captured string
	manager := f.svc.agentManager.(*mockAgentManager)
	manager.launchAgentFunc = func(_ context.Context, req *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
		captured = req.CausingRunID
		return &executor.LaunchAgentResponse{AgentExecutionID: "exec-" + req.SessionID}, nil
	}

	f.svc.drainDeferredCeilingLaunches(ctx)

	require.Equal(t, "office-run-1", captured)
	require.False(t, f.ceilingDeferred(t))
	require.Len(t, settler.replayed, 1)
	require.Equal(t, "office-run-1", settler.replayed[0].causingRunID)
	require.NotEmpty(t, settler.replayed[0].sessionID)
	require.Empty(t, settler.dropped)
}

func TestCeilingDeferredNotificationsRequireCausingRunID(t *testing.T) {
	settler := &recordingCeilingSettler{}
	svc := &Service{ceilingDeferredLaunchSettler: settler}
	deferral := models.CeilingDeferral{Payload: map[string]interface{}{}}

	svc.notifyCeilingDeferredLaunchReplayed(context.Background(), "task", deferral, ceilingReplayResult{})
	svc.notifyCeilingDeferredLaunchDropped(context.Background(), "task", deferral, "superseded", "detail")
	require.Empty(t, settler.replayed)
	require.Empty(t, settler.dropped)

	deferral.Payload[ceilingPayloadCausingRunIDKey] = "office-run-1"
	svc.notifyCeilingDeferredLaunchReplayed(context.Background(), "task", deferral,
		ceilingReplayResult{sessionID: "session-1", turnID: "turn-1"})
	svc.notifyCeilingDeferredLaunchDropped(context.Background(), "task", deferral, "superseded", "detail")

	require.Len(t, settler.replayed, 1)
	require.Equal(t, "office-run-1", settler.replayed[0].causingRunID)
	require.Equal(t, "session-1", settler.replayed[0].sessionID)
	require.Equal(t, "turn-1", settler.replayed[0].turnID)
	require.Len(t, settler.dropped, 1)
	require.Equal(t, "office-run-1", settler.dropped[0].causingRunID)
	require.Equal(t, "superseded", settler.dropped[0].reasonCode)
}
