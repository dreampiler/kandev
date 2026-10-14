package models

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPendingStepSignalSourceContextEndsAtCommit(t *testing.T) {
	source := &StepCompletionSource{Entry: StepCompletionEntry{StepID: "step-a", TransitionID: 42}}
	ctx := WithStepCompletionSource(context.Background(), "task-a", "step-a", source)
	entry, guarded := StepCompletionEntryFromContext(ctx, "task-a")
	require.True(t, guarded)
	require.Equal(t, int64(42), entry.TransitionID)
	_, guarded = StepCompletionEntryFromContext(ctx, "task-b")
	require.False(t, guarded)
	ctx = WithStepCompletionEntry(ctx, "", StepCompletionEntry{})
	_, guarded = StepCompletionEntryFromContext(ctx, "task-a")
	require.False(t, guarded)
	_, captured := StepCompletionSourceFromContext(ctx, "task-a", "step-a")
	require.False(t, captured)
}

func TestPendingStepSignalPreservesEntryIdentity(t *testing.T) {
	signal, ok := LoadPendingStepSignal(map[string]interface{}{
		SessionMetaKeyPendingStepCompletion: map[string]interface{}{
			"step_id": "step-a", "entry_transition_id": float64(42),
			"source": StepCompletionSourceAgent, "summary": "entry complete",
		},
	})
	require.True(t, ok)
	encoded, err := json.Marshal(signal)
	require.NoError(t, err)
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, float64(42), decoded["entry_transition_id"], "reloading the bag must preserve its server-owned entry")
}

func TestPendingStepSignalEntryMatchingKeepsLegacyFallback(t *testing.T) {
	entryTime := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	entry := StepCompletionEntry{StepID: "step-a", TransitionID: 42, OccurredAt: entryTime}
	cases := []struct {
		name   string
		signal PendingStepCompletionSignal
		entry  StepCompletionEntry
		want   bool
	}{
		{"same identity despite clock", PendingStepCompletionSignal{StepID: "step-a", EntryTransitionID: 42, SignaledAt: entryTime.Add(-time.Hour)}, entry, true},
		{"old identity despite new date", PendingStepCompletionSignal{StepID: "step-a", EntryTransitionID: 41, SignaledAt: entryTime.Add(time.Hour)}, entry, false},
		{"unknown current identity", PendingStepCompletionSignal{StepID: "step-a", EntryTransitionID: 42}, StepCompletionEntry{StepID: "step-a"}, false},
		{"old legacy date", PendingStepCompletionSignal{StepID: "step-a", SignaledAt: entryTime.Add(-time.Hour)}, entry, false},
		{"current legacy date", PendingStepCompletionSignal{StepID: "step-a", SignaledAt: entryTime.Add(time.Hour)}, entry, true},
		{"undated legacy is ambiguous", PendingStepCompletionSignal{StepID: "step-a"}, entry, false},
		{"different step", PendingStepCompletionSignal{StepID: "step-b", EntryTransitionID: 42}, entry, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, PendingStepSignalMatchesEntry(tc.signal, tc.entry))
		})
	}
}
