package models

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

const TurnMetaKeyWorkflowStepEntryAtStart = "workflow_step_entry_at_start"

var ErrStepCompletionEntryChanged = errors.New("workflow entry changed before completion signal was recorded")

var ErrStepCompletionEntryUnknown = errors.New("workflow completion entry could not be established")

// StepCompletionEntry identifies one server-owned arrival in a workflow step.
type StepCompletionEntry struct {
	StepID       string
	TransitionID int64
	OccurredAt   time.Time
}

// PendingStepSignalMatchesEntry uses identity first and dates only for legacy bags.
func PendingStepSignalMatchesEntry(signal PendingStepCompletionSignal, entry StepCompletionEntry) bool {
	if entry.StepID == "" || signal.StepID != entry.StepID {
		return false
	}
	if signal.EntryTransitionID > 0 {
		return entry.TransitionID > 0 && signal.EntryTransitionID == entry.TransitionID
	}
	if entry.TransitionID == 0 && entry.OccurredAt.IsZero() {
		return true
	}
	return !entry.OccurredAt.IsZero() && !signal.SignaledAt.IsZero() && !signal.SignaledAt.Before(entry.OccurredAt)
}

type stepCompletionEntryContextKey struct{}

type stepCompletionEntryGuard struct {
	taskID string
	stepID string
	entry  StepCompletionEntry
	source *StepCompletionSource
}

// StepCompletionSource is the immutable bag selected before a consuming move.
type StepCompletionSource struct {
	Entry  StepCompletionEntry
	Signal PendingStepCompletionSignal
	Value  interface{}
}

// WithStepCompletionEntry binds a consuming mutation to its captured source entry.
func WithStepCompletionEntry(ctx context.Context, taskID string, entry StepCompletionEntry) context.Context {
	return context.WithValue(ctx, stepCompletionEntryContextKey{}, stepCompletionEntryGuard{taskID: taskID, entry: entry})
}

func StepCompletionEntryFromContext(ctx context.Context, taskID string) (StepCompletionEntry, bool) {
	guard, ok := ctx.Value(stepCompletionEntryContextKey{}).(stepCompletionEntryGuard)
	return guard.entry, ok && guard.taskID == taskID && guard.entry.TransitionID > 0
}

func WithStepCompletionSource(ctx context.Context, taskID, stepID string, source *StepCompletionSource) context.Context {
	guard := stepCompletionEntryGuard{taskID: taskID, stepID: stepID, source: source}
	if source != nil {
		guard.entry = source.Entry
	}
	return context.WithValue(ctx, stepCompletionEntryContextKey{}, guard)
}

func StepCompletionSourceFromContext(ctx context.Context, taskID, stepID string) (*StepCompletionSource, bool) {
	guard, ok := ctx.Value(stepCompletionEntryContextKey{}).(stepCompletionEntryGuard)
	return guard.source, ok && guard.taskID == taskID && guard.stepID == stepID
}

// StepCompletionEntryIDFromAny reads the typed and JSON-rehydrated identities.
func StepCompletionEntryIDFromAny(value interface{}) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		if id := int64(v); id > 0 && float64(id) == v {
			return id
		}
	case json.Number:
		id, _ := v.Int64()
		return id
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	}
	return 0
}
