package review

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// unparseableInference models a reviewer that returns prose with no recoverable
// findings envelope for a configured model, and a clean review for every other
// model. It records the identity of every call so the fallback order is visible.
type unparseableInference struct {
	mu        sync.Mutex
	badModels map[string]bool
	calls     []ReviewerIdentity
}

func (f *unparseableInference) Run(_ context.Context, identity ReviewerIdentity, _ string, _ string) (*PromptResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, identity)
	bad := f.badModels[identity.Model]
	f.mu.Unlock()
	if bad {
		return &PromptResult{Response: "the reviewer returned prose with no findings envelope", Model: identity.Model}, nil
	}
	return &PromptResult{Response: okResponse("a.go", 2), Model: identity.Model}, nil
}

func (f *unparseableInference) models() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Model)
	}
	return out
}

// TestRunner_UnparseableResponseRetriesNextCandidate: a reviewer reply with no
// findings array is finalized failed (reason unparseable) and retried once on
// the dynamic route's next candidate, which completes. The retry is linked to
// the run it replaces.
func TestRunner_UnparseableResponseRetriesNextCandidate(t *testing.T) {
	inference := &unparseableInference{badModels: map[string]bool{"opencode/big-pickle": true}}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle", "space-bunny"), inference, time.Minute)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-unparseable", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("the unparseable run should be failed, got %q", got)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeUnparseableResponse {
		t.Fatalf("expected a recorded unparseable failure, got %+v ok=%v", failure, ok)
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
		t.Fatalf("expected the retry summary to link the unparseable run %s, got %+v ok=%v", run.ID, completed, ok)
	}
}

// TestRunner_UnparseableResponseWithoutNextCandidateDoesNotRetry: when the route
// has no other candidate, the unparseable run is finalized and nothing starts.
func TestRunner_UnparseableResponseWithoutNextCandidateDoesNotRetry(t *testing.T) {
	inference := &unparseableInference{badModels: map[string]bool{"opencode/big-pickle": true}}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle"), inference, time.Minute)

	run, err := runner.Run(context.Background(), RunRequest{TaskID: "task-unparseable-no-next", AgentProfileID: "review-dynamic"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if failure, ok := store.lastFailure(); !ok || failure.code != CodeUnparseableResponse {
		t.Fatalf("expected a recorded unparseable failure, got %+v ok=%v", failure, ok)
	}
	if store.runCount() != 1 {
		t.Fatalf("no candidate remains, so no retry may start, got %d runs", store.runCount())
	}
	if got := store.statusOf(run.ID); got != models.ReviewRunFailed {
		t.Fatalf("expected failed, got %q", got)
	}
}

// TestRunner_RetryUnparseableDoesNotRetryAgain: a retry that is also unparseable
// is finalized and never retried a second time.
func TestRunner_RetryUnparseableDoesNotRetryAgain(t *testing.T) {
	inference := &unparseableInference{badModels: map[string]bool{
		"opencode/big-pickle":       true,
		"opencode/space-bunny-free": true,
	}}
	runner, store := newTimeoutHarness(t, dynamicRoute("big-pickle", "space-bunny"), inference, time.Minute)

	if _, err := runner.Run(context.Background(), RunRequest{TaskID: "task-unparseable-twice", AgentProfileID: "review-dynamic"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if store.runCount() != 2 {
		t.Fatalf("expected exactly one retry even when it is also unparseable, got %d runs", store.runCount())
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

// TestIsRetryableFailure pins the fallback predicate: only a deadline or an
// unparseable reply retries; an ordinary failure and a cancellation do not.
func TestIsRetryableFailure(t *testing.T) {
	if !isRetryableFailure(context.Background(), context.DeadlineExceeded) {
		t.Fatal("a deadline error must be retryable")
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if !isRetryableFailure(expired, errors.New("pass overran")) {
		t.Fatal("a pass whose context is expired must be retryable")
	}
	if !isRetryableFailure(context.Background(), ErrUnparseableResponse) {
		t.Fatal("an unparseable reviewer reply must be retryable")
	}
	if isRetryableFailure(context.Background(), errors.New("provider exploded")) {
		t.Fatal("an ordinary failure must not be retryable")
	}
	if isRetryableFailure(context.Background(), context.Canceled) {
		t.Fatal("a cancellation must not be retryable")
	}
}
