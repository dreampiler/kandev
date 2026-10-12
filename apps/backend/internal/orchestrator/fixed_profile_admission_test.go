package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

const (
	fixedProfileTaskID      = "task-fixed-profile-circuit"
	fixedProfileSessionID   = "session-fixed-profile-circuit"
	fixedProfileExecutionID = "execution-fixed-profile"
	fixedProfileID          = "fixed-profile"
	fixedProfileSiblingID   = "fixed-profile-sibling"
)

type fixedProfileFixture struct {
	svc  *Service
	repo interface {
		GetTaskSession(context.Context, string) (*models.TaskSession, error)
		UpdateTaskSession(context.Context, *models.TaskSession) error
	}
	resolver *agentruntime.ProfileExecutionResolver
	circuits *dynamicruntime.CircuitRegistry
	now      *time.Time
}

// newFixedProfileFixture builds a task session that runs one concrete profile
// with no route generation, the shape every fixed-profile session has.
func newFixedProfileFixture(t *testing.T, state models.TaskSessionState) fixedProfileFixture {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, fixedProfileTaskID, fixedProfileSessionID, state)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, fixedProfileTaskID, v1.TaskStateInProgress)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), taskRepo, &mockAgentManager{})
	now := time.Now()
	clock := &now
	circuits := dynamicruntime.NewCircuitRegistry(dynamicruntime.WithCircuitClock(func() time.Time { return *clock }))
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, "dynamic-unused", []workflowDynamicCandidate{
		{executionProfileID: fixedProfileID, enabled: true},
		{executionProfileID: fixedProfileSiblingID, enabled: true},
	}, dynamicruntime.WithPersistence(repo), dynamicruntime.WithCircuitRegistry(circuits),
		dynamicruntime.WithClock(func() time.Time { return *clock }))
	svc.SetProfileExecutionResolver(resolver)
	session, err := repo.GetTaskSession(ctx, fixedProfileSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = fixedProfileID
	session.ExecutionProfileID = fixedProfileID
	session.RouteGeneration = 0
	session.AgentExecutionID = fixedProfileExecutionID
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("project fixed session: %v", err)
	}
	return fixedProfileFixture{svc: svc, repo: repo, resolver: resolver, circuits: circuits, now: clock}
}

func fixedProfileFailure(code routingerr.Code) *routingerr.Error {
	return &routingerr.Error{
		Code: code, Class: routingerr.ClassForCode(code),
		Confidence: routingerr.ConfHigh, FallbackAllowed: true,
	}
}

func (f fixedProfileFixture) fail(ctx context.Context, data watcher.AgentEventData, code routingerr.Code) {
	data.TaskID = fixedProfileTaskID
	data.SessionID = fixedProfileSessionID
	f.svc.routeDynamicAgentFailure(ctx, data, fixedProfileFailure(code))
}

// completeTurn runs one fixed-profile prompt that produces output and ends
// without a provider error.
func (f fixedProfileFixture) completeTurn(ctx context.Context, promptGeneration uint64) {
	f.svc.beginPromptAttempt(fixedProfileSessionID, fixedProfileExecutionID, promptGeneration, false)
	f.svc.observePromptAttempt(fixedProfileSessionID, fixedProfileExecutionID, promptGeneration, true, false)
	f.svc.recordDynamicResourceTurnSuccess(ctx, fixedProfileSessionID, fixedProfileExecutionID, fixedProfileID, promptGeneration)
}

func (f fixedProfileFixture) suspension(ctx context.Context, t *testing.T, profileID string) dynamicruntime.CandidateSuspension {
	t.Helper()
	got, known := f.resolver.CandidateSuspension(ctx, profileID, *f.now)
	if !known {
		t.Fatalf("suspension of %s is unknown", profileID)
	}
	return got
}

// A usage limit on a fixed profile records its circuit, even though the
// session has no route generation and the execution already produced output.
// The next prompt into the still-running execution is refused with the scope,
// code and reset instant, and the session keeps its profile and running turn.
func TestFixedProfileUsageLimitRefusesNextPrompt(t *testing.T) {
	ctx := context.Background()
	f := newFixedProfileFixture(t, models.TaskSessionStateRunning)

	f.fail(ctx, watcher.AgentEventData{
		AgentExecutionID: fixedProfileExecutionID, ExecutionProfileID: fixedProfileID,
		EvidenceKnown: true, OutputObserved: true,
	}, routingerr.CodeQuotaLimited)

	got := f.suspension(ctx, t, fixedProfileID)
	if !got.Blocked() || got.Code != routingerr.CodeQuotaLimited || got.Until.IsZero() {
		t.Fatalf("fixed profile suspension = %#v, want a quota_limited block with a reset instant", got)
	}
	err := f.svc.admitFixedProfileDispatch(ctx, fixedProfileSessionID, fixedProfileID)
	var refused *FixedProfileSuspendedError
	if !errors.As(err, &refused) || refused.ExecutionProfileID != fixedProfileID {
		t.Fatalf("admit = %v, want a FixedProfileSuspendedError for %s", err, fixedProfileID)
	}
	session, err := f.repo.GetTaskSession(ctx, fixedProfileSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	if session.State != models.TaskSessionStateRunning {
		t.Errorf("session state = %s, want the running turn left alone", session.State)
	}
	if session.ExecutionProfileID != fixedProfileID || session.AgentProfileID != fixedProfileID {
		t.Errorf("session profiles = %s/%s, want the fixed profile unchanged", session.AgentProfileID, session.ExecutionProfileID)
	}
	for _, want := range []string{"code=quota_limited", "reset=" + got.Until.UTC().Format(time.RFC3339)} {
		if !strings.Contains(session.ErrorMessage, want) {
			t.Errorf("session message %q does not name %q", session.ErrorMessage, want)
		}
	}
}

// A new launch of a suspended fixed profile is refused before the executor
// starts anything, and a session that never started waits for input.
func TestFixedProfileLaunchRefusedWhileCircuitOpen(t *testing.T) {
	ctx := context.Background()
	f := newFixedProfileFixture(t, models.TaskSessionStateCreated)
	f.resolver.RecordResourceFailure(ctx, fixedProfileID, fixedProfileFailure(routingerr.CodeQuotaLimited))

	execution, err := f.svc.launchPreparedSessionWithDynamicFallback(ctx, nil, fixedProfileSessionID, executor.LaunchOptions{
		AgentProfileID: fixedProfileID, StartAgent: true, Prompt: "hello",
	})
	var refused *FixedProfileSuspendedError
	if execution != nil || !errors.As(err, &refused) {
		t.Fatalf("launch = %#v, %v; want a fixed-profile refusal", execution, err)
	}
	session, err := f.repo.GetTaskSession(ctx, fixedProfileSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	if session.State != models.TaskSessionStateWaitingForInput || !strings.Contains(session.ErrorMessage, "resource suspended") {
		t.Fatalf("session = %s %q, want waiting for input with the suspension named", session.State, session.ErrorMessage)
	}
}

// The refusal does not depend on reading the session: a missing session still
// refuses instead of dereferencing a nil session.
func TestFixedProfileRefusalWithoutReadableSession(t *testing.T) {
	ctx := context.Background()
	f := newFixedProfileFixture(t, models.TaskSessionStateRunning)
	f.resolver.RecordResourceFailure(ctx, fixedProfileID, fixedProfileFailure(routingerr.CodeQuotaLimited))

	err := f.svc.admitFixedProfileDispatch(ctx, "session-that-does-not-exist", fixedProfileID)
	var refused *FixedProfileSuspendedError
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "code=quota_limited") {
		t.Fatalf("admit = %v, want a refusal naming quota_limited", err)
	}
}

// An account restriction on a fixed profile is held on that profile only: it
// survives time and real output, a sibling profile stays admitted, and only
// the owner release admits the profile again.
func TestFixedProfileAccountBlockHeldUntilOwnerRelease(t *testing.T) {
	ctx := context.Background()
	f := newFixedProfileFixture(t, models.TaskSessionStateRunning)

	f.fail(ctx, watcher.AgentEventData{
		AgentExecutionID: fixedProfileExecutionID, ExecutionProfileID: fixedProfileID,
	}, routingerr.CodeAccountBlocked)

	*f.now = f.now.Add(30 * 24 * time.Hour)
	f.completeTurn(ctx, 1)

	got := f.suspension(ctx, t, fixedProfileID)
	if !got.Blocked() || got.Scope != dynamicruntime.SuspensionScopeProfile || !got.Until.IsZero() {
		t.Fatalf("held profile after time and output = %#v, want a held profile block", got)
	}
	if err := f.svc.admitFixedProfileDispatch(ctx, fixedProfileSessionID, fixedProfileID); err == nil ||
		!strings.Contains(err.Error(), "held until owner release") {
		t.Fatalf("admit while held = %v, want an owner-release refusal", err)
	}
	if err := f.svc.admitFixedProfileDispatch(ctx, fixedProfileSessionID, fixedProfileSiblingID); err != nil {
		t.Fatalf("sibling profile admit = %v, want admitted", err)
	}
	if released, err := f.resolver.ReleaseAccountBlock(ctx, fixedProfileID); err != nil || !released {
		t.Fatalf("owner release = %v, %v; want released", released, err)
	}
	if err := f.svc.admitFixedProfileDispatch(ctx, fixedProfileSessionID, fixedProfileID); err != nil {
		t.Fatalf("admit after owner release = %v, want admitted", err)
	}
}

// After the reset instant passes the fixed profile is admitted again, and its
// first clean turn clears the strike history so the next failure does not
// inherit an escalated backoff. A block still running is never cleared.
// Admission reads wall time, so the engine clock starts in the past and is
// later moved to the present rather than beyond it.
func TestFixedProfileOutputClearsExpiredSuspensionOnly(t *testing.T) {
	ctx := context.Background()
	f := newFixedProfileFixture(t, models.TaskSessionStateRunning)
	*f.now = time.Now().Add(-2 * time.Hour)
	key := dynamicruntime.ResourceKey(dynamicruntime.ScopeProfile, fixedProfileID)
	f.circuits.Open(key, f.now.Add(time.Hour), routingerr.CodeQuotaLimited)

	f.completeTurn(ctx, 1)
	if got := f.circuits.Inspect(key, *f.now); got.State != dynamicruntime.ResourceWaiting {
		t.Fatalf("running block after output = %#v, want it kept", got)
	}

	*f.now = time.Now()
	if err := f.svc.admitFixedProfileDispatch(ctx, fixedProfileSessionID, fixedProfileID); err != nil {
		t.Fatalf("admit after reset = %v, want admitted", err)
	}
	f.completeTurn(ctx, 2)
	if got := f.circuits.Inspect(key, *f.now); got.State != dynamicruntime.ResourceAvailable || got.Strikes != 0 {
		t.Fatalf("expired block after output = %#v, want cleared", got)
	}
}

// Fixed-profile attribution charges only the profile the failed execution
// ran. A failure from another execution with no profile on the event, and a
// failure of a dynamic session, record nothing through the fixed path.
func TestFixedProfileFailureAttributionFences(t *testing.T) {
	ctx := context.Background()
	f := newFixedProfileFixture(t, models.TaskSessionStateRunning)

	f.svc.recordFixedProfileFailure(ctx, watcher.AgentEventData{
		TaskID: fixedProfileTaskID, SessionID: fixedProfileSessionID, AgentExecutionID: "execution-other",
	}, fixedProfileFailure(routingerr.CodeQuotaLimited))
	if got := f.suspension(ctx, t, fixedProfileID); got.Blocked() {
		t.Fatalf("foreign execution failure charged the fixed profile: %#v", got)
	}

	session, err := f.repo.GetTaskSession(ctx, fixedProfileSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = "dynamic-unused"
	session.RouteGeneration = 3
	if err := f.repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("project dynamic session: %v", err)
	}
	f.svc.recordFixedProfileFailure(ctx, watcher.AgentEventData{
		TaskID: fixedProfileTaskID, SessionID: fixedProfileSessionID,
		AgentExecutionID: fixedProfileExecutionID, ExecutionProfileID: fixedProfileID,
	}, fixedProfileFailure(routingerr.CodeQuotaLimited))
	if got := f.suspension(ctx, t, fixedProfileID); got.Blocked() {
		t.Fatalf("fixed path charged a dynamic session's candidate: %#v", got)
	}
}
