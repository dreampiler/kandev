package orchestrator

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/childstall"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

func TestMissingSignalNativeEntryIdentityPrecedesClock(t *testing.T) {
	entryAt := time.Now().UTC()
	for _, tc := range []struct {
		name       string
		entryID    int64
		signalID   int64
		signaledAt time.Time
		knownTime  bool
		want       bool
	}{
		{"accepted current entry with older clock", 5, 5, entryAt.Add(-time.Hour), true, true},
		{"earlier entry with newer clock", 5, 4, entryAt.Add(time.Hour), true, false},
		{"current identity without dates", 5, 5, time.Time{}, false, true},
		{"earlier identity without dates", 5, 4, time.Time{}, false, false},
		{"unknown entry retains accepted signal", 0, 5, entryAt.Add(-time.Hour), false, true},
		{"undated legacy retains accepted signal", 5, 0, time.Time{}, true, true},
		{"old legacy date", 5, 0, entryAt.Add(-time.Hour), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := &models.TaskSession{Metadata: map[string]interface{}{
				models.SessionMetaKeyPendingStepCompletion: models.PendingStepCompletionSignal{
					StepID: "step-a", EntryTransitionID: tc.signalID, SignaledAt: tc.signaledAt,
				},
			}}
			evidence := childstall.Evidence{Start: models.ChildStallStart{TransitionID: tc.entryID}}
			var newest time.Time
			observeCompletionSignalState(session, "step-a", entryAt, tc.knownTime, &evidence, &newest)
			require.Equal(t, tc.want, evidence.PendingStepSignal)
		})
	}
}
