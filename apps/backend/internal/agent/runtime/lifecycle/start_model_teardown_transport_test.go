package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

// A SetModel that fails because the agent stream is already gone is a
// transport or teardown race, not a model the provider refused. Reporting it as
// model_selection_failed/application_failed names the requested model as the
// cause on the session's error card.
func TestStartModelTransportFailureIsNotReportedAsModelApplicationFailure(t *testing.T) {
	transportErrors := map[string]error{
		"stream not connected": errors.New("set model: agent stream not connected"),
		"peer disconnected":    errors.New("jsonrpc internal error: peer disconnected before response"),
		"connection closed":    errors.New("set model: connection closed"),
	}
	branches := []struct {
		name   string
		policy StartModelPolicy
		state  *CachedModelState
	}{
		{
			name:   "requested model",
			policy: StartModelPolicy{Model: "saved-model", RequireExactModel: true},
			state:  modelState("saved-model"),
		},
		{
			name:   "unique variation",
			policy: StartModelPolicy{Model: "saved-model"},
			state:  modelState("saved-model[1m]"),
		},
		{
			name:   "advertised fallback",
			policy: StartModelPolicy{Model: "saved-model", FallbackModel: "alternate"},
			state:  modelState("alternate"),
		},
	}
	for _, branch := range branches {
		for name, transportErr := range transportErrors {
			t.Run(branch.name+"/"+name, func(t *testing.T) {
				applier := &fakeModelApplier{errs: []error{transportErr}}
				_, err := applyStartModelPolicy(
					context.Background(), newPolicyTestLogger(), applier, branch.state, branch.policy,
				)
				if !errors.Is(err, transportErr) {
					t.Fatalf("error = %v, want the transport error passed through", err)
				}
				var failure *BootstrapFailure
				if errors.As(err, &failure) {
					t.Fatalf("transport failure was reported as model selection evidence: code=%q reason=%q",
						failure.Code, failure.Reason)
				}
			})
		}
	}
}

// A provider that answers SetModel with a real error still fails closed as a
// model application failure.
func TestStartModelProviderRejectionStaysModelApplicationFailure(t *testing.T) {
	applier := &fakeModelApplier{errs: []error{errors.New("provider rejected selection")}}
	_, err := applyStartModelPolicy(
		context.Background(), newPolicyTestLogger(), applier,
		modelState("saved-model"), StartModelPolicy{Model: "saved-model", RequireExactModel: true},
	)
	var failure *BootstrapFailure
	if !errors.As(err, &failure) || failure.Reason != models.AgentErrorCauseReasonApplicationFailed {
		t.Fatalf("error = %v, want application_failed bootstrap evidence", err)
	}
}
