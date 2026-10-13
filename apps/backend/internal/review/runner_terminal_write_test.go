package review

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	taskservice "github.com/kandev/kandev/internal/task/service"
)

// scriptedStore wraps fakeStore to script terminal-write failures and record the
// context each attempt wrote with, so the retry and detachment contracts are
// observable without a database.
type scriptedStore struct {
	*fakeStore
	mu                   sync.Mutex
	failBudget           int
	failErr              error
	completeBudget       int
	completeErr          error
	failAttempts         int
	completeAttempts     int
	failWriteCtxErrs     []error
	completeWriteCtxErrs []error
}

func (s *scriptedStore) FailRun(ctx context.Context, runID, code, message string, durationMs int) (*models.TaskReviewRun, error) {
	s.mu.Lock()
	s.failAttempts++
	s.failWriteCtxErrs = append(s.failWriteCtxErrs, ctx.Err())
	fail := s.failBudget > 0
	if fail {
		s.failBudget--
	}
	err := s.failErr
	s.mu.Unlock()
	if fail {
		return nil, err
	}
	return s.fakeStore.FailRun(ctx, runID, code, message, durationMs)
}

func (s *scriptedStore) CompleteRun(ctx context.Context, req taskservice.CompleteRunRequest) (*models.TaskReviewRun, error) {
	s.mu.Lock()
	s.completeAttempts++
	s.completeWriteCtxErrs = append(s.completeWriteCtxErrs, ctx.Err())
	fail := s.completeBudget > 0
	if fail {
		s.completeBudget--
	}
	err := s.completeErr
	s.mu.Unlock()
	if fail {
		return nil, err
	}
	return s.fakeStore.CompleteRun(ctx, req)
}

// newScriptedHarness builds a runner over a scripted store with a zero backoff,
// so retry tests stay fast while still exercising multiple attempts.
func newScriptedHarness(t *testing.T) (*Runner, *scriptedStore) {
	t.Helper()
	store := &scriptedStore{fakeStore: newFakeStore()}
	runner := NewRunner(RunnerDeps{
		Store:       store,
		Resolver:    NewResolver(nil, &fakeUtility{found: true, enabled: true, agentID: "claude-acp", model: "haiku"}, nil),
		Changes:     &fakeChangeSource{uncommitted: map[string]any{"a.go": fileEntry("a.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")}},
		Inference:   &fakeInference{responses: []string{okResponse("a.go", 2)}},
		Prompts:     &fakePrompts{},
		TaskContext: &fakeTaskContext{ctx: PromptContext{TaskTitle: "T"}},
		Sessions:    &fakeSessions{sessionID: "sess-1"},
		Logger:      testLogger(t),
	})
	runner.writePolicy = terminalWritePolicy{attempts: 3, timeout: time.Second, backoff: 0}
	runner.Start(context.Background())
	t.Cleanup(runner.Stop)
	return runner, store
}

// TestRunner_TerminalWriteRetriesOnDetachedContext covers
// AC-AGENTS-REVIEW-RUN-RECOVERY-001.1: a failure write that loses to a busy
// writer twice, then succeeds, still records the terminal state, and every
// attempt writes on a context detached from the (expired) pass context.
func TestRunner_TerminalWriteRetriesOnDetachedContext(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.failBudget = 2
	store.failErr = errors.New("database is locked")

	run, err := store.CreateRun(context.Background(), taskservice.CreateRunRequest{TaskID: "task-retry"})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := store.MarkRunRunning(context.Background(), run.ID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}

	expired, cancel := context.WithTimeout(context.Background(), 0)
	cancel()
	_ = runner.fail(expired, run.ID, fmt.Errorf("%w: provider exploded", ErrExecutionFailed), time.Now())

	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("run should be failed after retry, got %q", got)
	}
	if store.failAttempts != 3 {
		t.Fatalf("expected 3 attempts (2 fail, 1 succeed), got %d", store.failAttempts)
	}
	for i, ctxErr := range store.failWriteCtxErrs {
		if ctxErr != nil {
			t.Fatalf("attempt %d wrote on a non-detached context: %v", i, ctxErr)
		}
	}
}

// TestRunner_CompletionWriteFailureRecordsFailedRun covers
// AC-AGENTS-REVIEW-RUN-RECOVERY-001.2: a completion write that fails on every
// attempt leaves a failed run with a reason, never a running one.
func TestRunner_CompletionWriteFailureRecordsFailedRun(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.completeBudget = 99
	store.completeErr = errors.New("database is locked")

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-complete-fail"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("a failed completion write must record failed, got %q", got)
	}
	if store.completeAttempts < 2 {
		t.Fatalf("expected the completion write retried, got %d attempts", store.completeAttempts)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeExecutionFailed {
		t.Fatalf("expected a recorded execution failure, got %+v ok=%v", failure, ok)
	}
}

// TestRunner_NormalCompletionWritesOnce guards that the retry path does not
// change the ordinary, first-attempt-success completion.
func TestRunner_NormalCompletionWritesOnce(t *testing.T) {
	runner, store := newScriptedHarness(t)
	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-ok"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.completeAttempts != 1 {
		t.Fatalf("expected 1 completion attempt, got %d", store.completeAttempts)
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunCompleted {
		t.Fatalf("expected completed, got %q", got)
	}
}

// TestRunner_AbandonedRunIsFinalizedAndNewReviewStarts covers
// AC-AGENTS-REVIEW-RUN-RECOVERY-001.3: a pending/running run past the
// progress-less limit is recorded failed and no longer blocks a new pass.
func TestRunner_AbandonedRunIsFinalizedAndNewReviewStarts(t *testing.T) {
	runner, store := newScriptedHarness(t)
	stale := &models.TaskReviewRun{
		ID: "run-stale", TaskID: "task-abandoned", Status: models.ReviewRunRunning,
		CreatedAt: time.Now().Add(-time.Hour),
	}
	store.runs[stale.ID] = stale
	store.active = stale

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-abandoned"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := store.statusOf("run-stale"); got != models.ReviewRunFailed {
		t.Fatalf("abandoned run should be failed, got %q", got)
	}
	if run.ID == "run-stale" {
		t.Fatal("the new request must not rejoin the abandoned run")
	}
	if got := store.runCount(); got != 2 {
		t.Fatalf("expected a new run alongside the abandoned one, got %d", got)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeAbandoned {
		t.Fatalf("expected the abandonment recorded with %q, got %+v ok=%v", CodeAbandoned, failure, ok)
	}
}

// TestRunner_FreshActiveRunIsRejoined guards that an in-limit active run is
// still returned unchanged, with no finalization and no duplicate pass.
func TestRunner_FreshActiveRunIsRejoined(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.active = &models.TaskReviewRun{
		ID: "run-fresh", TaskID: "task-fresh", Status: models.ReviewRunRunning, CreatedAt: time.Now(),
	}

	got, err := runner.Launch(context.Background(), RunRequest{TaskID: "task-fresh"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if got.ID != "run-fresh" {
		t.Fatalf("expected the fresh run rejoined, got %q", got.ID)
	}
	if store.runCount() != 0 {
		t.Fatal("a fresh active run must not be duplicated")
	}
	if store.failAttempts != 0 {
		t.Fatal("a fresh active run must not be finalized")
	}
}

// TestRunner_ActiveRunWithUnknownAgeIsRejoined guards the fail-open rule: a run
// with no recorded age is never classified as abandoned.
func TestRunner_ActiveRunWithUnknownAgeIsRejoined(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.active = &models.TaskReviewRun{
		ID: "run-unknown", TaskID: "task-unknown", Status: models.ReviewRunRunning,
	}

	got, err := runner.Launch(context.Background(), RunRequest{TaskID: "task-unknown"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if got.ID != "run-unknown" {
		t.Fatalf("expected the active run rejoined, got %q", got.ID)
	}
	if store.failAttempts != 0 {
		t.Fatal("an active run with no recorded age must not be finalized")
	}
}

// TestRunner_TerminalRunIsNotAbandoned guards that the predicate never touches a
// run that already reached a terminal state.
func TestRunner_TerminalRunIsNotAbandoned(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.active = &models.TaskReviewRun{
		ID: "run-done", TaskID: "task-done", Status: models.ReviewRunCompleted,
		CreatedAt: time.Now().Add(-time.Hour),
	}

	got, err := runner.Launch(context.Background(), RunRequest{TaskID: "task-done"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if got.ID != "run-done" {
		t.Fatalf("expected the terminal run returned, got %q", got.ID)
	}
	if store.failAttempts != 0 {
		t.Fatal("a terminal run must not be finalized")
	}
}

// TestRunner_AbandonedRunStaysWhenFinalizeFails guards the single-active-run
// invariant: if the abandonment cannot be recorded, the existing run is
// reported rather than a duplicate pass started.
func TestRunner_AbandonedRunStaysWhenFinalizeFails(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.failBudget = 99
	store.failErr = errors.New("database is locked")
	store.active = &models.TaskReviewRun{
		ID: "run-stuck", TaskID: "task-stuck", Status: models.ReviewRunRunning,
		CreatedAt: time.Now().Add(-time.Hour),
	}

	got, err := runner.Launch(context.Background(), RunRequest{TaskID: "task-stuck"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if got.ID != "run-stuck" {
		t.Fatalf("expected the existing run returned when finalization failed, got %q", got.ID)
	}
	if store.runCount() != 0 {
		t.Fatal("no new run may start while the abandoned row could not be finalized")
	}
	if store.failAttempts < 2 {
		t.Fatalf("expected the abandonment write retried, got %d attempts", store.failAttempts)
	}
}

// TestRunner_ActiveRunReadErrorFailsClosed covers the review finding on the
// downstream `ActiveRun` read: an unreadable active-run set may already hold a
// row, so a launch must not proceed to create a second run.
func TestRunner_ActiveRunReadErrorFailsClosed(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.activeErr = errors.New("database is locked")

	if _, err := runner.Launch(context.Background(), RunRequest{TaskID: "task-read-error"}); err == nil {
		t.Fatal("Launch must fail closed when the active-run read fails")
	}
	if store.runCount() != 0 {
		t.Fatalf("no run may be created when the active-run read failed, got %d", store.runCount())
	}
}

// TestRunner_CompletionWriteDeadlineKeepsExecutionFailureReason guards that a
// storage write that timed out on a detached context is not mislabeled as the
// pass exceeding its time budget; only the pass's own deadline is a timeout.
func TestRunner_CompletionWriteDeadlineKeepsExecutionFailureReason(t *testing.T) {
	runner, store := newScriptedHarness(t)
	store.completeBudget = 99
	store.completeErr = context.DeadlineExceeded

	if _, err := runner.Run(context.Background(), RunRequest{TaskID: "task-complete-deadline"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeExecutionFailed {
		t.Fatalf("a completion-write timeout must keep the execution-failure reason, got %+v ok=%v", failure, ok)
	}
}
