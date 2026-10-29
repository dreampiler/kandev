package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/automation"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/task/models"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

const ceilingReplayRetainedFailureLog = "ceiling retry replay failed for a non-ceiling reason; will retry on a later sweep"

type launchFailureReplayFixture struct {
	svc    *Service
	repo   *tasksqlite.Repository
	logs   *observer.ObservedLogs
	taskID string

	mu       sync.Mutex
	launches int
}

func (f *launchFailureReplayFixture) launchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches
}

func (f *launchFailureReplayFixture) ceilingDeferred(t *testing.T) bool {
	t.Helper()
	return models.HasCeilingDeferredIntent(&models.Task{Metadata: map[string]interface{}{
		models.MetaKeyDeferredLaunch: deferredLaunchOf(t, f.svc, f.taskID),
	}})
}

// newLaunchFailureReplayFixture queues an automatic workflow start behind the
// ceiling and makes every agent launch fail with launchErr.
func newLaunchFailureReplayFixture(t *testing.T, taskID string, launchErr error) *launchFailureReplayFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	repo := setupTestRepo(t)
	_ = repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws1", Name: "Test", CreatedAt: now, UpdatedAt: now})
	_ = repo.CreateWorkflow(ctx, &models.Workflow{ID: "wf1", WorkspaceID: "ws1", Name: "Test Workflow", CreatedAt: now, UpdatedAt: now})
	require.NoError(t, repo.CreateTask(ctx, &models.Task{
		ID: taskID, WorkspaceID: "ws1", WorkflowID: "wf1", WorkflowStepID: "step-auto",
		Title: "launch failure", Description: "desc", State: v1.TaskStateScheduling,
		CreatedAt: now, UpdatedAt: now,
	}))

	steps := newMockStepGetter()
	steps.steps["step-auto"] = &wfmodels.WorkflowStep{
		ID: "step-auto", WorkflowID: "wf1", Name: "Auto",
		Events: wfmodels.StepEvents{OnEnter: []wfmodels.OnEnterAction{{Type: wfmodels.OnEnterAutoStartAgent}}},
	}
	tasks := newMockTaskRepo()
	seedMockTaskState(tasks, taskID, v1.TaskStateScheduling)

	core, logs := observer.New(zapcore.WarnLevel)
	observed, err := logger.NewFromZap(zap.New(core))
	require.NoError(t, err)

	f := &launchFailureReplayFixture{repo: repo, logs: logs, taskID: taskID}
	agent := &mockAgentManager{
		repoForExecutionLookup: repo,
		launchAgentFunc: func(context.Context, *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			f.mu.Lock()
			f.launches++
			f.mu.Unlock()
			return nil, launchErr
		},
	}
	f.svc = createTestServiceWithScheduler(repo, steps, tasks, agent)
	f.svc.logger = observed
	f.svc.sessionCeiling = newSessionCeilingController(unlimitedSessionCeiling, nil, nil)

	record := models.CeilingRecordKeys(models.CeilingDeferral{
		Kind: models.CeilingLaunchStart,
		Payload: map[string]interface{}{
			metaKeyPrompt:         "start the step",
			metaKeyAgentProfileID: "profile-1",
			metaKeyWorkflowStepID: "step-auto",
			"origin":              string(launchOriginAutomatic),
		},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   now,
	})
	_, _, err = repo.SetTaskDeferredLaunchIfUnchanged(ctx, taskID, tasksqlite.AbsentDeferredLaunch(), record)
	require.NoError(t, err)
	return f
}

// @covers AC-AGENTS-SESSION-CEILING-001.11
func TestCeilingSweepDropsAStartWhoseLaunchFailsCommandValidation(t *testing.T) {
	ctx := context.Background()
	f := newLaunchFailureReplayFixture(t, "launch-invalid-task",
		&routingerr.BuiltCommandValidationError{Command: "agent", Cause: errors.New("agent command cannot be empty")})

	f.svc.drainDeferredCeilingLaunches(ctx)
	require.Equal(t, 1, f.launchCount(), "the first sweep replays the queued start once")
	require.False(t, f.ceilingDeferred(t),
		"a start whose launch fails command validation must be dropped, not kept for the next sweep")

	f.svc.drainDeferredCeilingLaunches(ctx)
	require.Equal(t, 1, f.launchCount(), "a dropped start must not launch again")
	sessions, err := f.repo.ListTaskSessions(ctx, f.taskID)
	require.NoError(t, err)
	require.Len(t, sessions, 1, "the invalid command must leave one failed session, not one per sweep")
}

// @covers AC-AGENTS-SESSION-CEILING-001.5
func TestCeilingSweepKeepsATransientLaunchFailureAndLogsItsCause(t *testing.T) {
	ctx := context.Background()
	f := newLaunchFailureReplayFixture(t, "launch-transient-task",
		errors.New("connection reset by peer while validate agent command: was pending"))

	f.svc.drainDeferredCeilingLaunches(ctx)
	require.Equal(t, 1, f.launchCount())
	require.True(t, f.ceilingDeferred(t), "a transient launch failure keeps the record for a later sweep")

	entries := f.logs.FilterMessage(ceilingReplayRetainedFailureLog).All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, f.taskID, fields["task_id"])
	require.Contains(t, fields["error"], "connection reset by peer", "the warning must carry the failure")
	require.NotEmpty(t, fields["detail"], "the warning must say which replay step failed")
}

// --- explicit deferred-launch discard -------------------------------------

// ceilingDiscardSettlementFailure is an AutomationService whose dispatch path
// always fails, so failQueuedAutomationRun cannot settle the run a queued start
// serves.
type ceilingDiscardSettlementFailure struct{ err error }

func (f ceilingDiscardSettlementFailure) GetAutomation(context.Context, string) (*automation.Automation, error) {
	return nil, nil
}

func (f ceilingDiscardSettlementFailure) RecordRun(context.Context, *automation.AutomationRun) error {
	return nil
}

func (f ceilingDiscardSettlementFailure) MarkRunFailedByTaskID(context.Context, string, string) error {
	return nil
}

func (f ceilingDiscardSettlementFailure) MarkRunSucceededByTaskID(context.Context, string) error {
	return nil
}

func (f ceilingDiscardSettlementFailure) DispatchRun(
	context.Context, string, automation.ThreadAction, string, func() (automation.RunDispatch, error),
) error {
	return f.err
}

// ceilingDiscardWriteFailure is a repository whose deferred-launch write always
// fails, so a clear cannot be proven to land.
type ceilingDiscardWriteFailure struct {
	*tasksqlite.Repository
	err error
}

func (r ceilingDiscardWriteFailure) SetTaskDeferredLaunchIfUnchanged(
	context.Context, string, interface{}, map[string]interface{},
) (bool, bool, error) {
	return false, false, r.err
}

func newDiscardTestTask(t *testing.T, repo *tasksqlite.Repository, taskID string) {
	t.Helper()
	require.NoError(t, repo.CreateTask(context.Background(), &models.Task{
		ID: taskID, Title: "discard", State: v1.TaskStateScheduling,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))
}

func seedCeilingDeferral(
	t *testing.T, svc *Service, taskID string, deferral models.CeilingDeferral, extra map[string]interface{},
) {
	t.Helper()
	record := models.CeilingRecordKeys(deferral)
	for key, value := range extra {
		record[key] = value
	}
	_, _, err := svc.repo.SetTaskDeferredLaunchIfUnchanged(
		context.Background(), taskID, tasksqlite.AbsentDeferredLaunch(), record,
	)
	require.NoError(t, err)
}

func discardTestDeferral(sessionID string, payload map[string]interface{}) models.CeilingDeferral {
	if payload == nil {
		payload = map[string]interface{}{metaKeySessionID: sessionID}
	}
	return models.CeilingDeferral{
		Kind:       models.CeilingLaunchResume,
		Payload:    payload,
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}
}

func hasCeilingDiscardIntent(record map[string]interface{}) bool {
	return models.HasCeilingDeferredIntent(&models.Task{Metadata: map[string]interface{}{
		models.MetaKeyDeferredLaunch: record,
	}})
}

func TestDiscardDeferredCeilingLaunchAlreadyClear(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	newDiscardTestTask(t, repo, "discard-clear")

	result, err := svc.DiscardDeferredCeilingLaunch(ctx, "discard-clear")
	require.NoError(t, err)
	require.Equal(t, models.DeferredLaunchDiscardAlreadyClear, result.Outcome)
	require.Empty(t, result.Reason, "a completed outcome carries no reason")
	require.Nil(t, deferredLaunchOf(t, svc, "discard-clear"), "a no-op must not write")
}

func TestDiscardDeferredCeilingLaunchRemovesOnlyTheCeilingHalf(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	newDiscardTestTask(t, repo, "discard-ok")
	require.NoError(t, repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "discard-ok-session", TaskID: "discard-ok", State: models.TaskSessionStateRunning,
	}))
	seedCeilingDeferral(t, svc, "discard-ok", discardTestDeferral("discard-ok-session", nil),
		map[string]interface{}{models.DeferredLaunchStartWhenUnblockedKey: true})

	result, err := svc.DiscardDeferredCeilingLaunch(ctx, "discard-ok")
	require.NoError(t, err)
	require.Equal(t, models.DeferredLaunchDiscardDiscarded, result.Outcome)

	after := deferredLaunchOf(t, svc, "discard-ok")
	require.False(t, hasCeilingDiscardIntent(after))
	require.Equal(t, true, after[models.DeferredLaunchStartWhenUnblockedKey],
		"discarding the ceiling half must not disturb another owner's key")

	sessions, err := repo.ListTaskSessions(ctx, "discard-ok")
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, models.TaskSessionStateRunning, sessions[0].State,
		"a discard must not start or stop the task's sessions")
}

func TestDiscardDeferredCeilingLaunchRetainsOnSettlementFailure(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	newDiscardTestTask(t, repo, "discard-settlement")
	svc.automationService = ceilingDiscardSettlementFailure{err: errors.New("dispatch unavailable")}
	payload := map[string]interface{}{
		metaKeySessionID:               "discard-settlement-session",
		ceilingPayloadAutomationRunKey: map[string]interface{}{"run_id": "run-settlement"},
	}
	seedCeilingDeferral(t, svc, "discard-settlement", discardTestDeferral("", payload), nil)

	result, err := svc.DiscardDeferredCeilingLaunch(ctx, "discard-settlement")
	require.NoError(t, err)
	require.Equal(t, models.DeferredLaunchDiscardRetained, result.Outcome)
	require.Equal(t, models.DeferredLaunchDiscardReasonSettlement, result.Reason)

	after := deferredLaunchOf(t, svc, "discard-settlement")
	require.True(t, hasCeilingDiscardIntent(after),
		"a failed settlement must leave the durable record recoverable")
}

func TestDiscardDeferredCeilingLaunchRetainsOnClearFailure(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	newDiscardTestTask(t, repo, "discard-clear-fail")
	seedCeilingDeferral(t, svc, "discard-clear-fail", discardTestDeferral("discard-clear-fail-session", nil), nil)
	svc.repo = ceilingDiscardWriteFailure{Repository: repo, err: errors.New("write failed")}

	result, err := svc.DiscardDeferredCeilingLaunch(ctx, "discard-clear-fail")
	require.NoError(t, err)
	require.Equal(t, models.DeferredLaunchDiscardRetained, result.Outcome)
	require.Equal(t, models.DeferredLaunchDiscardReasonClear, result.Reason)
	require.True(t, hasCeilingDiscardIntent(deferredLaunchOf(t, svc, "discard-clear-fail")))
}

func TestDiscardDeferredCeilingLaunchLeavesAClaimedRecord(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	newDiscardTestTask(t, repo, "discard-claimed")
	claim := map[string]interface{}{
		"id": "claim-live", "owner": ceilingClaimOwnerReplay,
		models.CeilingLaunchClaimExpiresAtKey: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano),
	}
	seedCeilingDeferral(t, svc, "discard-claimed", discardTestDeferral("discard-claimed-session", nil),
		map[string]interface{}{models.CeilingLaunchClaimKey: claim})

	result, err := svc.DiscardDeferredCeilingLaunch(ctx, "discard-claimed")
	require.NoError(t, err)
	require.Equal(t, models.DeferredLaunchDiscardRetained, result.Outcome)
	require.Equal(t, models.DeferredLaunchDiscardReasonClaimInFlight, result.Reason)

	after := deferredLaunchOf(t, svc, "discard-claimed")
	require.Equal(t, claim, after[models.CeilingLaunchClaimKey], "another dispatcher's claim is left untouched")
}

func TestDiscardDeferredCeilingLaunchRetainsUnreadableRecord(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	newDiscardTestTask(t, repo, "discard-unreadable")
	seedCeilingDeferral(t, svc, "discard-unreadable", models.CeilingDeferral{
		Kind:       models.CeilingLaunchKind("outside-the-closed-set"),
		Payload:    map[string]interface{}{},
		Origin:     string(launchOriginAutomatic),
		ReasonCode: ceilingReasonRefused,
		QueuedAt:   time.Now().UTC(),
	}, nil)

	result, err := svc.DiscardDeferredCeilingLaunch(ctx, "discard-unreadable")
	require.NoError(t, err)
	require.Equal(t, models.DeferredLaunchDiscardRetained, result.Outcome)
	require.Equal(t, models.DeferredLaunchDiscardReasonRecordUnreadable, result.Reason)
	require.NotNil(t, deferredLaunchOf(t, svc, "discard-unreadable"))
}

func TestClearCeilingDeferredRecordOutcomeLeavesASuccessor(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	newDiscardTestTask(t, repo, "discard-successor")
	original := discardTestDeferral("discard-successor-old", nil)
	successor := discardTestDeferral("discard-successor-new", nil)
	successor.QueuedAt = original.QueuedAt.Add(time.Second)
	seedCeilingDeferral(t, svc, "discard-successor", successor, nil)

	result := svc.clearCeilingDeferredRecordOutcome(ctx, "discard-successor", &original, true)
	require.Equal(t, ceilingClearSuperseded, result)

	after := deferredLaunchOf(t, svc, "discard-successor")
	got, err := models.ReadCeilingDeferral(after)
	require.NoError(t, err)
	require.Equal(t, successor.Kind, got.Kind)
	require.Equal(t, "discard-successor-new", sessionIDFromCeilingPayload(got))
}
