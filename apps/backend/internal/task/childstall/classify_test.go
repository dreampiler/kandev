package childstall

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

const classifyTestTransitionID = int64(7)

func qualifiedEvidence() Evidence {
	return Evidence{
		Start:              models.ChildStallStart{ParentTaskID: "parent", WorkspaceID: "ws", TransitionID: classifyTestTransitionID},
		Settlement:         "",
		Child:              &ChildSnapshot{ParentID: "parent", WorkspaceID: "ws", State: v1.TaskStateInProgress, LatestTransitionID: classifyTestTransitionID},
		SessionState:       models.TaskSessionStateWaitingForInput,
		StepRequiresSignal: true,
		SettledFor:         time.Minute,
	}
}

func TestClassifyRoutesEachCause(t *testing.T) {
	policy := Policy{SettleGrace: time.Second, SignalWait: time.Minute}
	cases := []struct {
		name        string
		mutate      func(*Evidence)
		wantOutcome Outcome
		wantCause   Cause
		wantReason  string
	}{
		{
			name:        "missing completion signal",
			mutate:      func(*Evidence) {},
			wantOutcome: OutcomeQualified,
			wantCause:   CauseMissingCompletionSignal,
		},
		{
			name:        "input required",
			mutate:      func(e *Evidence) { e.PendingInputs = []PendingInput{{MessageID: "m1"}} },
			wantOutcome: OutcomeQualified,
			wantCause:   CauseInputRequired,
		},
		{
			name: "quota",
			mutate: func(e *Evidence) {
				e.SessionState = models.TaskSessionStateFailed
				e.SessionErrorClass = QuotaErrorClass
			},
			wantOutcome: OutcomeQualified,
			wantCause:   CauseQuota,
		},
		{
			name:        "execution error",
			mutate:      func(e *Evidence) { e.SessionState = models.TaskSessionStateFailed },
			wantOutcome: OutcomeQualified,
			wantCause:   CauseExecutionError,
		},
		{
			name:        "optional signal is ordinary",
			mutate:      func(e *Evidence) { e.StepRequiresSignal = false },
			wantOutcome: OutcomeSuppressed,
			wantReason:  ReasonOrdinary,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := qualifiedEvidence()
			tc.mutate(&evidence)
			got := Classify(evidence, policy)
			if got.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", got.Outcome, tc.wantOutcome)
			}
			if tc.wantCause != "" && got.Cause != tc.wantCause {
				t.Fatalf("cause = %q, want %q", got.Cause, tc.wantCause)
			}
			if tc.wantReason != "" && got.Reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}
