package review

import (
	"context"
	"fmt"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

type launchFailInference struct{ seen []string }

func (f *launchFailInference) Run(_ context.Context, id ReviewerIdentity, _, _ string) (*PromptResult, error) {
	f.seen = append(f.seen, id.ProfileID)
	if id.ProfileID == "big-pickle" {
		return nil, fmt.Errorf("%w: ACP initialize failed", ErrAgentUnavailable)
	}
	return &PromptResult{Response: `{"summary":"clean","findings":[]}`}, nil
}

func TestRunnerDynamicRouteContinuesAfterStructuralLaunchFailure(t *testing.T) {
	h := newRunnerHarness(t, map[string]any{"a.go": fileEntry("a.go", "@@ -1 +1 @@\n-old\n+new\n", "", "")}, nil)
	r := NewResolver(reviewProfiles(), nil, nil)
	r.Routes = &fakeRoute{candidates: map[string][]string{"review-dynamic": {"big-pickle", "sol", "opus"}}}
	h.runner.resolver = r
	f := &launchFailInference{}
	h.runner.inference = f
	run, err := h.runner.Run(t.Context(), RunRequest{TaskID: "task", SessionID: "sess-1", AgentProfileID: "review-dynamic", Origin: writerOrigin(), OriginRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	if h.store.statusOf(run.ID) != models.ReviewRunCompleted {
		t.Fatal("structural launch failure ended the whole review")
	}
	if len(f.seen) != 2 || f.seen[0] != "big-pickle" || f.seen[1] != "opus" {
		t.Fatalf("route attempts = %v", f.seen)
	}
	if h.store.runs[run.ID].Model != "opus" {
		t.Fatal("run did not record the reviewer that completed it")
	}
}
