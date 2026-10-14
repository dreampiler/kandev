package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/task/models"
)

func TestBootstrapFailureForClassifiesAgentStartup(t *testing.T) {
	for _, phase := range []routingerr.Phase{routingerr.PhaseProcessStart, routingerr.PhaseSessionInit} {
		t.Run(string(phase), func(t *testing.T) {
			startupErr := routingerr.NewAgentStartupFailure(phase, "opencode-acp",
				errors.New("ACP initialize failed: peer disconnected before response"))
			failure := bootstrapFailureFor(&AgentExecution{}, startupErr)
			if failure == nil {
				t.Fatal("expected a classified bootstrap failure")
			}
			if failure.Code != models.AgentErrorCauseCodeTransportUnavailable {
				t.Fatalf("code = %q, want %q", failure.Code, models.AgentErrorCauseCodeTransportUnavailable)
			}
			if failure.Operation != models.AgentErrorCauseOperationStart {
				t.Fatalf("operation = %q, want %q", failure.Operation, models.AgentErrorCauseOperationStart)
			}
			for _, want := range []string{string(phase), "opencode-acp", "peer disconnected"} {
				if !strings.Contains(failure.Detail, want) {
					t.Fatalf("detail = %q, want it to contain %q", failure.Detail, want)
				}
			}
			if !errors.Is(failure, startupErr) {
				t.Error("classified failure lost errors.Is access to the startup error")
			}
			cause, ok := failure.SafeAgentErrorCause(models.AgentErrorCauseOperationStart)
			if !ok {
				t.Fatal("expected a durable cause")
			}
			if cause.Code != models.AgentErrorCauseCodeTransportUnavailable ||
				!strings.Contains(cause.Detail, string(phase)) {
				t.Fatalf("durable cause = %+v, want transport_unavailable carrying the phase", cause)
			}
		})
	}
}

func TestBootstrapFailureForClassifiesManagedRuntimeStartup(t *testing.T) {
	managed := &routingerr.ManagedRuntimeStartupError{
		Code:    routingerr.CodeNetworkUnavailable,
		Details: "npx install failed for the managed runtime",
	}
	failure := bootstrapFailureFor(&AgentExecution{}, managed)
	if failure.Code != models.AgentErrorCauseCodeTransportUnavailable {
		t.Fatalf("code = %q, want %q", failure.Code, models.AgentErrorCauseCodeTransportUnavailable)
	}
	if !strings.Contains(failure.Detail, string(managed.Code)) ||
		!strings.Contains(failure.Detail, "npx install failed") {
		t.Fatalf("detail = %q, want the managed-runtime code and details", failure.Detail)
	}
}

func TestBootstrapFailureForKeepsUnknownForUnclassifiedErrors(t *testing.T) {
	failure := bootstrapFailureFor(&AgentExecution{}, errors.New("a failure the classifier does not recognize"))
	if failure.Code != models.AgentErrorCauseCodeUnknown {
		t.Fatalf("code = %q, want %q", failure.Code, models.AgentErrorCauseCodeUnknown)
	}
	if failure.Detail != bootstrapFailureDetail(models.AgentErrorCauseCodeUnknown) {
		t.Fatalf("detail = %q, want the generic unknown detail", failure.Detail)
	}
}

func TestAgentStartupFailureDetailRedactsAndBounds(t *testing.T) {
	secret := "sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	startupErr := routingerr.NewAgentStartupFailure(routingerr.PhaseSessionInit, "opencode-acp",
		errors.New("provider rejected the request: "+secret))
	failure := bootstrapFailureFor(&AgentExecution{}, startupErr)
	if strings.Contains(failure.Detail, secret) {
		t.Fatalf("detail leaked a credential: %q", failure.Detail)
	}
	if !strings.Contains(failure.Detail, "sk-***") {
		t.Fatalf("detail = %q, want the redacted credential marker", failure.Detail)
	}

	longErr := routingerr.NewAgentStartupFailure(routingerr.PhaseProcessStart, "opencode-acp",
		errors.New(strings.Repeat("diagnostic ", 400)))
	longFailure := bootstrapFailureFor(&AgentExecution{}, longErr)
	if len(longFailure.Detail) > maxBootstrapCauseDetailBytes+128 {
		t.Fatalf("detail length = %d, want the cause bounded near %d bytes",
			len(longFailure.Detail), maxBootstrapCauseDetailBytes)
	}
	if !utf8.ValidString(longFailure.Detail) {
		t.Fatalf("detail is not valid UTF-8: %q", longFailure.Detail)
	}

	multibyte := routingerr.NewAgentStartupFailure(routingerr.PhaseSessionInit, "opencode-acp",
		errors.New(strings.Repeat("가", 400)))
	multibyteFailure := bootstrapFailureFor(&AgentExecution{}, multibyte)
	if !utf8.ValidString(multibyteFailure.Detail) {
		t.Fatalf("multibyte detail was cut mid-rune: %q", multibyteFailure.Detail)
	}
	if len(multibyteFailure.Detail) > maxBootstrapCauseDetailBytes+128 {
		t.Fatalf("multibyte detail length = %d, want it bounded near %d",
			len(multibyteFailure.Detail), maxBootstrapCauseDetailBytes)
	}
}

func TestBootstrapFailureForKeepsTimeoutForWrappedDeadline(t *testing.T) {
	managed := &routingerr.ManagedRuntimeStartupError{
		Code:    routingerr.CodeAgentRuntime,
		Details: "managed runtime bootstrap timed out",
		Cause:   context.DeadlineExceeded,
	}
	failure := bootstrapFailureFor(&AgentExecution{}, managed)
	if failure.Code != models.AgentErrorCauseCodeTimeout {
		t.Fatalf("code = %q, want %q for a wrapped deadline", failure.Code, models.AgentErrorCauseCodeTimeout)
	}
}
