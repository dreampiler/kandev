package orchestrator

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

func TestWorkflowReplacementSurvivesTerminalPredecessor(t *testing.T) {
	for _, terminal := range []models.TaskSessionState{
		models.TaskSessionStateCancelled, models.TaskSessionStateCompleted, models.TaskSessionStateFailed,
	} {
		t.Run(string(terminal), func(t *testing.T) {
			ctx := context.Background()
			fixture := newProfileSwitchFixture(t, models.WorkflowProfileSessionStartPolicyNew, models.WorkflowProfileSessionEndPolicyPark)
			fixture.svc.messageQueue = nil
			require.NoError(t, fixture.repo.UpdateTaskSessionState(ctx, fixture.current.ID, terminal, "source ended"))
			destination, err := fixture.svc.createNewSessionForStepWithEndPolicy(ctx, "t1", fixture.current, "profile-b", models.WorkflowProfileSessionEndPolicyPark)
			require.NoError(t, err, "a terminal predecessor already satisfies its departure without being parked again")
			require.NotNil(t, destination)
			primary, err := fixture.repo.GetActiveTaskSessionByTaskID(ctx, "t1")
			require.NoError(t, err)
			require.Equal(t, destination.ID, primary.ID)
			source, err := fixture.repo.GetTaskSession(ctx, fixture.current.ID)
			require.NoError(t, err)
			require.Equal(t, terminal, source.State, "the predecessor must never be reopened")
		})
	}
}

func TestWorkflowDepartureWaitsForNonterminalSourceStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		f := newProfileSwitchFixture(t, models.WorkflowProfileSessionStartPolicyNew, models.WorkflowProfileSessionEndPolicyPark)
		stopEntered := make(chan struct{})
		stopRelease := make(chan struct{})
		defer close(stopRelease)
		f.agentMgr.stopAgentFunc = func(context.Context, string, bool) error {
			close(stopEntered)
			<-stopRelease
			return nil
		}
		done := make(chan error, 1)
		go func() {
			_, err := f.svc.finishWorkflowProfileSwitchSource(ctx, "t1", f.current, models.WorkflowProfileSessionEndPolicyPark)
			done <- err
		}()
		<-stopEntered
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("unguarded departure returned before its source teardown completed")
		default:
		}
	})
}
