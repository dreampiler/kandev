package handlers

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

type stepCompletionEntryReader interface {
	GetTaskStepCompletionEntry(context.Context, string) (models.StepCompletionEntry, error)
}

func (h *Handlers) stepCompletionLaunchEntry(ctx context.Context, taskID, sessionID, fallback string) (models.StepCompletionEntry, error) {
	entry := models.StepCompletionEntry{StepID: fallback}
	if reader, ok := h.sessionRepo.(stepCompletionEntryReader); ok {
		var err error
		entry, err = reader.GetTaskStepCompletionEntry(ctx, taskID)
		if err != nil {
			return entry, err
		}
		if entry.StepID != fallback {
			return entry, models.ErrStepCompletionEntryChanged
		}
		if entry.TransitionID == 0 {
			return entry, models.ErrStepCompletionEntryUnknown
		}
	}
	reader, ok := h.sessionRepo.(stepCompletionTurnReader)
	if !ok {
		return entry, nil
	}
	turns, err := reader.ListTurnsBySession(ctx, sessionID)
	if err != nil || len(turns) == 0 {
		return entry, err
	}
	return stepCompletionEntryForTurn(turns[len(turns)-1], entry)
}

func stepCompletionEntryForTurn(turn *models.Turn, current models.StepCompletionEntry) (models.StepCompletionEntry, error) {
	if turn == nil {
		return current, errors.New("latest turn is missing")
	}
	stepID := models.StringFromAny(turn.Metadata[models.TurnMetaKeyWorkflowStepIDAtStart])
	if stepID == "" {
		return current, errors.New("latest turn has no workflow-step stamp")
	}
	if stepID != current.StepID {
		return models.StepCompletionEntry{StepID: stepID}, nil
	}
	entryID := models.StepCompletionEntryIDFromAny(turn.Metadata[models.TurnMetaKeyWorkflowStepEntryAtStart])
	if entryID > 0 {
		if current.TransitionID != entryID {
			return current, models.ErrStepCompletionEntryChanged
		}
		return current, nil
	}
	if current.TransitionID > 0 {
		if turn.StartedAt.IsZero() || current.OccurredAt.IsZero() {
			return current, models.ErrStepCompletionEntryUnknown
		}
		if turn.StartedAt.Before(current.OccurredAt) {
			return current, models.ErrStepCompletionEntryChanged
		}
	}
	return current, nil
}

func stepCompletionEntryErrorResponse(msg *ws.Message, err error) (*ws.Message, bool, error) {
	if !errors.Is(err, models.ErrStepCompletionEntryChanged) && !errors.Is(err, models.ErrStepCompletionEntryUnknown) {
		return nil, false, nil
	}
	message := err.Error() + ". No signal was recorded. End this turn and resume this session for the current entry. " +
		"Retrying in this turn cannot recover. Do not move the task solely to bypass this error."
	response, encodeErr := ws.NewError(msg.ID, msg.Action, ws.ErrorCodeValidation, message, nil)
	return response, true, encodeErr
}
