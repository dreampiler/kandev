package backendapp

import (
	"context"
	"errors"
	"testing"

	utility "github.com/kandev/kandev/internal/agentctl/server/utility"
	"github.com/kandev/kandev/internal/review"
)

type reviewLaunchResponse struct{ response *utility.PromptResponse }

func (f reviewLaunchResponse) ExecuteInferencePrompt(context.Context, string, string, string, string) (*utility.PromptResponse, error) {
	return f.response, nil
}
func (f reviewLaunchResponse) ExecuteInferenceProfilePrompt(context.Context, string, string, string) (*utility.PromptResponse, error) {
	return f.response, nil
}

func TestReviewInferenceSeparatesLaunchFailureFromPromptFailure(t *testing.T) {
	for _, launchFailed := range []bool{false, true} {
		r := reviewInference{session: reviewLaunchResponse{response: &utility.PromptResponse{LaunchFailed: launchFailed, Error: "synthetic failure"}}}
		_, err := r.Run(t.Context(), review.ReviewerIdentity{ProfileID: "reviewer"}, "session", "review")
		if errors.Is(err, review.ErrAgentUnavailable) != launchFailed {
			t.Fatalf("launchFailed=%v, error=%v", launchFailed, err)
		}
	}
}
