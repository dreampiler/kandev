package orchestrator

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
)

type unlaunchedWorkflowEntryFixture struct {
	svc             *Service
	repo            *sqliterepo.Repository
	session         *models.TaskSession
	launchedSession *string
}

func newUnlaunchedWorkflowEntryFixture(t *testing.T) unlaunchedWorkflowEntryFixture {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task1", "session1", models.TaskSessionStateWaitingForInput)
	session, err := repo.GetTaskSession(ctx, "session1")
	require.NoError(t, err)
	session.AgentProfileID = "profile1"
	require.NoError(t, repo.UpdateTaskSession(ctx, session))
	getter := newMockStepGetter()
	step := &wfmodels.WorkflowStep{ID: "step1", WorkflowID: "wf1", Name: "Work"}
	getter.steps[step.ID] = step
	tasks := newMockTaskRepo()
	tasks.tasks["task1"] = &v1.Task{ID: "task1", Title: "Task", State: v1.TaskStateCreated}
	launchedSession := ""
	manager := &mockAgentManager{
		repoForExecutionLookup: repo,
		launchAgentFunc: func(_ context.Context, req *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			launchedSession = req.SessionID
			return &executor.LaunchAgentResponse{AgentExecutionID: "execution1"}, nil
		},
	}
	svc := createTestServiceWithScheduler(repo, getter, tasks, manager)
	svc.messageCreator = &mockMessageCreator{}
	return unlaunchedWorkflowEntryFixture{svc: svc, repo: repo, session: session, launchedSession: &launchedSession}
}

func TestWorkflowEntryStartsWaitingSessionWithoutExecutor(t *testing.T) {
	for _, withRecordedPrompt := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty conversation", true: "initial route selection refused"}[withRecordedPrompt], func(t *testing.T) {
			ctx := context.Background()
			f := newUnlaunchedWorkflowEntryFixture(t)
			if withRecordedPrompt {
				f.session.RouteState = dynamicRouteStatusWaiting
				f.session.RouteReason = "no_eligible_candidate"
				require.NoError(t, f.repo.UpdateTaskSession(ctx, f.session))
				recordUnlaunchedEntryPrompt(t, f, "initial-prompt")
			}
			step := &wfmodels.WorkflowStep{ID: "step1", WorkflowID: "wf1", Name: "Work"}
			err := f.svc.autoStartStepPrompt(ctx, "task1", f.session, step, "Do the work", false, false, nil)
			require.NoError(t, err, "a never-launched waiting session needs StartCreatedSession, not native resume")
			require.Equal(t, "session1", *f.launchedSession)
			sessions, err := f.repo.ListTaskSessions(ctx, "task1")
			require.NoError(t, err)
			require.Len(t, sessions, 1)
		})
	}
}

func TestWorkflowEntryDoesNotColdStartHistoricalConversationWithoutExecutor(t *testing.T) {
	ctx := context.Background()
	f := newUnlaunchedWorkflowEntryFixture(t)
	recordUnlaunchedEntryPrompt(t, f, "previous-prompt")
	step := &wfmodels.WorkflowStep{ID: "step1", WorkflowID: "wf1", Name: "Work"}
	err := f.svc.autoStartStepPrompt(ctx, "task1", f.session, step, "Do the work", false, false, nil)
	require.Error(t, err)
	require.Empty(t, *f.launchedSession)
}

func recordUnlaunchedEntryPrompt(t *testing.T, f unlaunchedWorkflowEntryFixture, id string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.repo.CreateTurn(ctx, &models.Turn{ID: id + "-turn", TaskID: "task1", TaskSessionID: "session1"}))
	require.NoError(t, f.repo.CreateMessage(ctx, &models.Message{
		ID: id, TurnID: id + "-turn", TaskID: "task1", TaskSessionID: "session1", AuthorType: "user", Content: "Initial instruction",
	}))
}
