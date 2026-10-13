package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	taskservice "github.com/kandev/kandev/internal/task/service"
)

// timeoutInference models a slow free reviewer: for a configured model it blocks
// until the pass context is done and returns the deadline, which is the failure
// a review pass that overruns its budget produces. Every other model answers at
// once. It records the identity of every call so the fallback order is visible.
type timeoutInference struct {
	mu            sync.Mutex
	timeoutModels map[string]bool
	calls         []ReviewerIdentity
}

func (f *timeoutInference) Run(ctx context.Context, identity ReviewerIdentity, _ string, _ string) (*PromptResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, identity)
	block := f.timeoutModels[identity.Model]
	f.mu.Unlock()
	if !block {
		return &PromptResult{Response: okResponse("a.go", 2), Model: identity.Model}, nil
	}
	<-ctx.Done()
	return nil, fmt.Errorf("inference: %w", ctx.Err())
}

func (f *timeoutInference) models() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Model)
	}
	return out
}

// newTimeoutHarness builds a runner over a dynamic reviewer route and a
// configurable inference, with a short pass timeout so a timeout test finishes
// quickly.
func newTimeoutHarness(t *testing.T, route DynamicRouteSelector, inference Inference, passTimeout time.Duration) (*Runner, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	return newTimeoutRunner(t, store, route, inference, passTimeout), store
}

func newTimeoutRunner(t *testing.T, store Store, route DynamicRouteSelector, inference Inference, passTimeout time.Duration) *Runner {
	t.Helper()
	resolver := NewResolver(reviewProfiles(), nil, nil)
	resolver.Routes = route
	runner := NewRunner(RunnerDeps{
		Store:       store,
		Resolver:    resolver,
		Changes:     &fakeChangeSource{uncommitted: map[string]any{"a.go": fileEntry("a.go", "@@ -1 +1,2 @@\n x\n+y\n", "", "")}},
		Inference:   inference,
		Prompts:     &fakePrompts{},
		TaskContext: &fakeTaskContext{ctx: PromptContext{TaskTitle: "T"}},
		Sessions:    &fakeSessions{sessionID: "sess-1"},
		Logger:      testLogger(t),
	})
	runner.passTimeout = passTimeout
	runner.writePolicy = terminalWritePolicy{attempts: 3, timeout: time.Second, backoff: 0}
	runner.Start(context.Background())
	t.Cleanup(runner.Stop)
	return runner
}

func dynamicRoute(candidates ...string) *fakeRoute {
	return &fakeRoute{candidates: map[string][]string{"review-dynamic": candidates}}
}

// TestRunner_TimeoutRetriesNextDynamicCandidate: a pass that ends on its deadline
// is finalized failed (reason timeout) and retried once on the dynamic route's
// next candidate, which completes. The retry is linked to the run it replaces.
func TestRunner_TimeoutRetriesNextDynamicCandidate(t *testing.T) {
	inference := &timeoutInference{timeoutModels: map[string]bool{"opencode/big-pickle": true}}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle", "space-bunny"), inference, 50*time.Millisecond)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-timeout", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("the timed-out run should be failed, got %q", got)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeTimeout {
		t.Fatalf("expected a recorded timeout failure, got %+v ok=%v", failure, ok)
	}
	if store.runCount() != 2 {
		t.Fatalf("expected exactly one automatic retry run, got %d", store.runCount())
	}
	if models := inference.models(); len(models) != 2 ||
		models[0] != "opencode/big-pickle" || models[1] != "opencode/space-bunny-free" {
		t.Fatalf("expected the route's next candidate to run the retry once, got %v", models)
	}
	var retry *models.TaskReviewRun
	for _, r := range store.runs {
		if r.ID != run.ID {
			retry = r
		}
	}
	if retry == nil || store.statusOf(retry.ID) != models.ReviewRunCompleted {
		t.Fatalf("expected the retry to complete, got %+v", retry)
	}
	if completed, ok := store.lastCompleted(); !ok || !strings.Contains(completed.Summary, run.ID) {
		t.Fatalf("expected the retry summary to link the timed-out run %s, got %+v ok=%v", run.ID, completed, ok)
	}
}

// TestRunner_TimeoutWithoutNextCandidateDoesNotRetry: when the route has no other
// candidate, the timed-out run is finalized and nothing else starts.
func TestRunner_TimeoutWithoutNextCandidateDoesNotRetry(t *testing.T) {
	inference := &timeoutInference{timeoutModels: map[string]bool{"opencode/big-pickle": true}}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle"), inference, 50*time.Millisecond)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-no-next", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeTimeout {
		t.Fatalf("expected a recorded timeout failure, got %+v ok=%v", failure, ok)
	}
	if store.runCount() != 1 {
		t.Fatalf("no candidate remains, so no retry may start, got %d runs", store.runCount())
	}
	if models := inference.models(); len(models) != 1 {
		t.Fatalf("expected a single attempt, got %v", models)
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("the timed-out run should be failed, got %q", got)
	}
}

// TestRunner_NonTimeoutFailureDoesNotRetry: only a deadline fallback retries; a
// provider error records failed and stops.
func TestRunner_NonTimeoutFailureDoesNotRetry(t *testing.T) {
	inference := &fakeInference{err: errors.New("provider exploded")}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle", "space-bunny"), inference, time.Minute)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-error", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeExecutionFailed {
		t.Fatalf("expected a recorded execution failure, got %+v ok=%v", failure, ok)
	}
	if store.runCount() != 1 {
		t.Fatalf("a non-timeout failure must not retry, got %d runs", store.runCount())
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("expected failed, got %q", got)
	}
}

// TestRunner_RetryTimeoutDoesNotRetryAgain: a retry that also times out is
// finalized and never retried a second time.
func TestRunner_RetryTimeoutDoesNotRetryAgain(t *testing.T) {
	inference := &timeoutInference{timeoutModels: map[string]bool{
		"opencode/big-pickle":       true,
		"opencode/space-bunny-free": true,
	}}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle", "space-bunny"), inference, 50*time.Millisecond)

	if _, err := runner.Run(context.Background(), RunRequest{TaskID: "task-retry-timeout", AgentProfileID: "review-dynamic"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.runCount() != 2 {
		t.Fatalf("expected exactly one retry even when it also times out, got %d runs", store.runCount())
	}
	if models := inference.models(); len(models) != 2 {
		t.Fatalf("expected exactly two attempts, got %v", models)
	}
	for id, r := range store.runs {
		if r.Status != models.ReviewRunFailed {
			t.Fatalf("run %s should be failed, got %q", id, r.Status)
		}
	}
}

// TestRunner_DynamicReviewerSuccessDoesNotRetry guards the ordinary path: a
// dynamic reviewer that answers on the first attempt starts no retry run.
func TestRunner_DynamicReviewerSuccessDoesNotRetry(t *testing.T) {
	inference := &timeoutInference{}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle", "space-bunny"), inference, time.Minute)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-ok-dynamic", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.runCount() != 1 {
		t.Fatalf("a successful review must not retry, got %d runs", store.runCount())
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunCompleted {
		t.Fatalf("expected completed, got %q", got)
	}
	if models := inference.models(); len(models) != 1 {
		t.Fatalf("expected a single attempt, got %v", models)
	}
}

// TestRunner_TimeoutDoesNotRetryWhenFinalizationFails covers the review finding
// on the fallback gate: a timed-out run whose finalization write keeps failing
// is not terminal, so starting a retry would leave two active rows for the task.
func TestRunner_TimeoutDoesNotRetryWhenFinalizationFails(t *testing.T) {
	store := &scriptedStore{fakeStore: newFakeStore(), failBudget: 99, failErr: errors.New("database is locked")}
	inference := &timeoutInference{timeoutModels: map[string]bool{"opencode/big-pickle": true}}
	runner := newTimeoutRunner(t, store, dynamicRoute("big-pickle", "space-bunny"), inference, 50*time.Millisecond)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-fail-stuck", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.runCount() != 1 {
		t.Fatalf("a retry must not start while the timed-out run is not terminal, got %d runs", store.runCount())
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunRunning {
		t.Fatalf("finalization failed, so the run should still be running, got %q", got)
	}
	if models := inference.models(); len(models) != 1 {
		t.Fatalf("expected a single attempt, got %v", models)
	}
}

// publishDeadlineStore makes the first PublishFindings call overrun the pass
// context, the way a pass that exhausts its budget while publishing fails, then
// serves later calls normally. It models the publish phase, which runs on the
// pass context rather than on a detached one.
type publishDeadlineStore struct {
	*fakeStore
	calls int
}

func (s *publishDeadlineStore) PublishFindings(ctx context.Context, req taskservice.PublishFindingsRequest) (*models.TaskReviewRun, []*models.TaskReviewFinding, error) {
	s.calls++
	if s.calls == 1 {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	return s.fakeStore.PublishFindings(ctx, req)
}

// TestRunner_PublishPhaseTimeoutRetriesNextCandidate covers the review finding on
// the publish phase: a pass whose deadline expires while findings are published
// is recorded as a timeout and takes the same one-shot fallback as an
// inference-phase timeout, rather than being recorded as a timeout with no retry.
func TestRunner_PublishPhaseTimeoutRetriesNextCandidate(t *testing.T) {
	store := &publishDeadlineStore{fakeStore: newFakeStore()}
	inference := &timeoutInference{}
	runner := newTimeoutRunner(t, store, dynamicRoute("big-pickle", "space-bunny"), inference, 50*time.Millisecond)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-publish-timeout", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.runCount() != 2 {
		t.Fatalf("a publish-phase deadline must start exactly one fallback, got %d runs", store.runCount())
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeTimeout {
		t.Fatalf("expected a recorded timeout failure, got %+v ok=%v", failure, ok)
	}
	if models := inference.models(); len(models) != 2 ||
		models[0] != "opencode/big-pickle" || models[1] != "opencode/space-bunny-free" {
		t.Fatalf("expected the fallback to run the next candidate once, got %v", models)
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("the timed-out run should be failed, got %q", got)
	}
	var retry *models.TaskReviewRun
	for _, r := range store.runs {
		if r.ID != run.ID {
			retry = r
		}
	}
	if retry == nil || store.statusOf(retry.ID) != models.ReviewRunCompleted {
		t.Fatalf("expected the retry to complete, got %+v", retry)
	}
}

// TestRunner_CompletionWriteFailureIsNotAPassTimeout covers the review finding on
// the completion phase: a completion write that fails while the pass context is
// expired is a storage failure, not a pass timeout, because the pass budget is
// decided before the write's retries can outlast it.
func TestRunner_CompletionWriteFailureIsNotAPassTimeout(t *testing.T) {
	store := newFakeStore()
	runner := newTimeoutRunner(t, store, dynamicRoute("big-pickle", "space-bunny"), &timeoutInference{}, time.Minute)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	run, err := store.CreateRun(context.Background(), taskservice.CreateRunRequest{TaskID: "task-completion-fail", AgentID: "a", Model: "m"})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runner.recordFailure(expired, run.ID, errors.New("database is locked"), time.Now(), false)
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeExecutionFailed {
		t.Fatalf("expected a storage execution failure, got %+v ok=%v", failure, ok)
	}

	run2, err := store.CreateRun(context.Background(), taskservice.CreateRunRequest{TaskID: "task-budget-fail", AgentID: "a", Model: "m"})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runner.recordFailure(expired, run2.ID, errors.New("deadline exceeded"), time.Now(), true)
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeTimeout {
		t.Fatalf("expected a recorded timeout, got %+v ok=%v", failure, ok)
	}
}

// TestRunner_RecordFailureNilCauseDoesNotPanic covers the review finding on the
// nil-cause path: recording a failure with no cause must not panic.
func TestRunner_RecordFailureNilCauseDoesNotPanic(t *testing.T) {
	store := newFakeStore()
	runner := newTimeoutRunner(t, store, dynamicRoute("big-pickle"), &timeoutInference{}, time.Minute)
	run, err := store.CreateRun(context.Background(), taskservice.CreateRunRequest{TaskID: "task-nil-cause", AgentID: "a", Model: "m"})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("recordFailure panicked on a nil cause: %v", r)
		}
	}()
	if ok := runner.recordFailure(context.Background(), run.ID, nil, time.Now(), false); !ok {
		t.Fatalf("expected a nil cause to be treated as nothing to record")
	}
}
