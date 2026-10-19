package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/orchestrator/watcher"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

const (
	fixedCircuitTaskID    = "task-fixed-circuit"
	fixedCircuitSessionID = "session-fixed-circuit"
	fixedCircuitExecution = "execution-fixed-circuit"
	fixedCircuitProfileID = "fixed-profile"
	fixedCircuitSiblingID = "fixed-profile-sibling"
)

// newFixedCircuitFixture builds a fixed-profile session: no route generation,
// one concrete profile served by one running execution.
func newFixedCircuitFixture(
	t *testing.T,
	state models.TaskSessionState,
) (*Service, *sqliterepo.Repository, *agentruntime.ProfileExecutionResolver) {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, fixedCircuitTaskID, fixedCircuitSessionID, state)
	svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{})
	resolver := newWorkflowDynamicProfileResolverWithCandidates(t, "dynamic-unused", []workflowDynamicCandidate{
		{executionProfileID: fixedCircuitProfileID, enabled: true},
		{executionProfileID: fixedCircuitSiblingID, enabled: true},
	})
	svc.SetProfileExecutionResolver(resolver)
	session, err := repo.GetTaskSession(ctx, fixedCircuitSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.AgentProfileID = fixedCircuitProfileID
	session.ExecutionProfileID = fixedCircuitProfileID
	session.AgentExecutionID = fixedCircuitExecution
	session.RouteGeneration = 0
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("UpdateTaskSession: %v", err)
	}
	return svc, repo, resolver
}

func fixedCircuitFailure(code routingerr.Code) *routingerr.Error {
	return &routingerr.Error{
		Code: code, Class: routingerr.ClassForCode(code),
		Confidence: routingerr.ConfHigh, FallbackAllowed: true,
	}
}

// A usage limit on a fixed profile records its circuit even though the session
// has no route generation and the execution already produced output. The next
// dispatch is refused with the scope, code and reset instant, and the running
// session keeps its state and profile.
func TestFixedProfileAdmissionRefusedWhileCircuitOpen(t *testing.T) {
	ctx := context.Background()
	svc, repo, resolver := newFixedCircuitFixture(t, models.TaskSessionStateRunning)
	svc.recordFixedProfileFailure(ctx, watcher.AgentEventData{
		TaskID: fixedCircuitTaskID, SessionID: fixedCircuitSessionID,
		AgentExecutionID: fixedCircuitExecution, ExecutionProfileID: fixedCircuitProfileID,
		EvidenceKnown: true, OutputObserved: true,
	}, fixedCircuitFailure(routingerr.CodeQuotaLimited))

	suspension, known := resolver.FixedProfileSuspension(ctx, fixedCircuitProfileID, time.Now())
	if !known || !suspension.Blocked() || suspension.Code != routingerr.CodeQuotaLimited || suspension.Until.IsZero() {
		t.Fatalf("recorded suspension = %#v, %v; want a quota_limited block with a reset instant", suspension, known)
	}

	err := svc.admitFixedProfileDispatch(ctx, fixedCircuitSessionID, fixedCircuitProfileID)
	var refused *FixedProfileSuspendedError
	if !errors.As(err, &refused) || refused.ExecutionProfileID != fixedCircuitProfileID {
		t.Fatalf("admit = %v, want a FixedProfileSuspendedError for %s", err, fixedCircuitProfileID)
	}
	session, err := repo.GetTaskSession(ctx, fixedCircuitSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	if session.State != models.TaskSessionStateRunning {
		t.Errorf("session state = %s, want the running turn left alone", session.State)
	}
	if session.ExecutionProfileID != fixedCircuitProfileID || session.AgentProfileID != fixedCircuitProfileID {
		t.Errorf("session profiles = %s/%s, want the fixed profile unchanged", session.AgentProfileID, session.ExecutionProfileID)
	}
	for _, want := range []string{"code=quota_limited", "reset=" + suspension.Until.UTC().Format(time.RFC3339)} {
		if !strings.Contains(session.ErrorMessage, want) {
			t.Errorf("session message %q does not name %q", session.ErrorMessage, want)
		}
	}
}

// A launch of a suspended fixed profile is refused through the concrete launch
// admission, and a session that never started waits for input.
func TestFixedProfileLaunchAdmissionMovesCreatedSession(t *testing.T) {
	ctx := context.Background()
	svc, repo, resolver := newFixedCircuitFixture(t, models.TaskSessionStateCreated)
	resolver.RecordFixedProfileFailure(ctx, fixedCircuitProfileID, fixedCircuitFailure(routingerr.CodeQuotaLimited))

	err := svc.admitFixedProfileDispatch(ctx, fixedCircuitSessionID, fixedCircuitProfileID)
	var refused *FixedProfileSuspendedError
	if !errors.As(err, &refused) || refused.ExecutionProfileID != fixedCircuitProfileID {
		t.Fatalf("admit = %v, want a fixed-profile refusal", err)
	}
	session, err := repo.GetTaskSession(ctx, fixedCircuitSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	if session.State != models.TaskSessionStateWaitingForInput || !strings.Contains(session.ErrorMessage, "resource suspended") {
		t.Fatalf("session = %s %q, want waiting for input with the suspension named", session.State, session.ErrorMessage)
	}
}

// A fixed-profile admission never depends on reading the session: a missing
// session still refuses instead of dereferencing it.
func TestFixedProfileAdmissionWithoutReadableSession(t *testing.T) {
	ctx := context.Background()
	svc, _, resolver := newFixedCircuitFixture(t, models.TaskSessionStateRunning)
	resolver.RecordFixedProfileFailure(ctx, fixedCircuitProfileID, fixedCircuitFailure(routingerr.CodeQuotaLimited))

	err := svc.admitFixedProfileDispatch(ctx, "session-that-does-not-exist", fixedCircuitProfileID)
	var refused *FixedProfileSuspendedError
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "code=quota_limited") {
		t.Fatalf("admit = %v, want a refusal naming quota_limited", err)
	}
}

// An account restriction on a fixed profile is held on that profile only: it
// survives the reset window, a sibling profile stays admitted, and only the
// owner release admits the profile again.
func TestFixedProfileAccountBlockHeldUntilOwnerRelease(t *testing.T) {
	ctx := context.Background()
	svc, _, resolver := newFixedCircuitFixture(t, models.TaskSessionStateRunning)
	svc.recordFixedProfileFailure(ctx, watcher.AgentEventData{
		TaskID: fixedCircuitTaskID, SessionID: fixedCircuitSessionID,
		AgentExecutionID: fixedCircuitExecution, ExecutionProfileID: fixedCircuitProfileID,
	}, fixedCircuitFailure(routingerr.CodeAccountBlocked))

	held, known := resolver.FixedProfileSuspension(ctx, fixedCircuitProfileID, time.Now().Add(365*24*time.Hour))
	if !known || !held.Blocked() || !held.Until.IsZero() {
		t.Fatalf("held suspension = %#v, %v; want an owner-release hold", held, known)
	}
	if err := svc.admitFixedProfileDispatch(ctx, fixedCircuitSessionID, fixedCircuitProfileID); err == nil ||
		!strings.Contains(err.Error(), "held until owner release") {
		t.Fatalf("admit while held = %v, want an owner-release refusal", err)
	}
	if err := svc.admitFixedProfileDispatch(ctx, fixedCircuitSessionID, fixedCircuitSiblingID); err != nil {
		t.Fatalf("sibling admit = %v, want admitted", err)
	}
	if released, err := resolver.ReleaseAccountBlock(ctx, fixedCircuitProfileID); err != nil || !released {
		t.Fatalf("owner release = %v, %v; want released", released, err)
	}
	if err := svc.admitFixedProfileDispatch(ctx, fixedCircuitSessionID, fixedCircuitProfileID); err != nil {
		t.Fatalf("admit after owner release = %v, want admitted", err)
	}
}

// Fixed-profile attribution charges only the profile the failed execution ran.
// A failure from another execution with no profile on the event, and a failure
// of a dynamic session, record nothing through the fixed path.
func TestFixedProfileFailureAttributionFences(t *testing.T) {
	ctx := context.Background()
	svc, repo, resolver := newFixedCircuitFixture(t, models.TaskSessionStateRunning)

	svc.recordFixedProfileFailure(ctx, watcher.AgentEventData{
		TaskID: fixedCircuitTaskID, SessionID: fixedCircuitSessionID, AgentExecutionID: "execution-other",
	}, fixedCircuitFailure(routingerr.CodeQuotaLimited))
	if suspension, _ := resolver.FixedProfileSuspension(ctx, fixedCircuitProfileID, time.Now()); suspension.Blocked() {
		t.Fatalf("a foreign execution charged the fixed profile: %#v", suspension)
	}

	session, err := repo.GetTaskSession(ctx, fixedCircuitSessionID)
	if err != nil {
		t.Fatalf("GetTaskSession: %v", err)
	}
	session.RouteGeneration = 3
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("UpdateTaskSession: %v", err)
	}
	svc.recordFixedProfileFailure(ctx, watcher.AgentEventData{
		TaskID: fixedCircuitTaskID, SessionID: fixedCircuitSessionID,
		AgentExecutionID: fixedCircuitExecution, ExecutionProfileID: fixedCircuitProfileID,
	}, fixedCircuitFailure(routingerr.CodeQuotaLimited))
	if suspension, _ := resolver.FixedProfileSuspension(ctx, fixedCircuitProfileID, time.Now()); suspension.Blocked() {
		t.Fatalf("the fixed path charged a dynamic session's candidate: %#v", suspension)
	}
}
