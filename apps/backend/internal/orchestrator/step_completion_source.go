package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

type completionEntryReader interface {
	GetTaskStepCompletionEntry(context.Context, string) (models.StepCompletionEntry, error)
}

type completionSignalSource = models.StepCompletionSource

func loadCompletionSignalSource(ctx context.Context, repo interface{}, taskID, stepID string, session *models.TaskSession) (*completionSignalSource, error) {
	if session == nil {
		return nil, nil
	}
	signal, ok := models.LoadPendingStepSignal(session.Metadata)
	if !ok || signal.StepID != stepID {
		return nil, nil
	}
	entry := models.StepCompletionEntry{StepID: stepID}
	if reader, ok := repo.(completionEntryReader); ok {
		var err error
		entry, err = reader.GetTaskStepCompletionEntry(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if entry.StepID == stepID && entry.TransitionID == 0 {
			return nil, models.ErrStepCompletionEntryUnknown
		}
	}
	if signal.EntryTransitionID == 0 && entry.TransitionID > 0 && (signal.SignaledAt.IsZero() || entry.OccurredAt.IsZero()) {
		return nil, models.ErrStepCompletionEntryUnknown
	}
	if !models.PendingStepSignalMatchesEntry(signal, entry) || entry.StepID != stepID {
		return nil, nil
	}
	return &completionSignalSource{Entry: entry, Signal: signal, Value: session.Metadata[models.SessionMetaKeyPendingStepCompletion]}, nil
}

func completionSignalFromContext(ctx context.Context, taskID, stepID string) (*completionSignalSource, bool) {
	return models.StepCompletionSourceFromContext(ctx, taskID, stepID)
}

func (s *Service) completionSignalForEntry(ctx context.Context, taskID, stepID string, session *models.TaskSession) *completionSignalSource {
	if source, captured := completionSignalFromContext(ctx, taskID, stepID); captured {
		return source
	}
	source, err := loadCompletionSignalSource(ctx, s.repo, taskID, stepID, session)
	if err != nil {
		s.logger.Warn("completion signal entry could not be established", zap.String("task_id", taskID), zap.String("step_id", stepID), zap.Error(err))
	}
	return source
}

func (s *Service) captureCompletionSignal(ctx context.Context, taskID, stepID string, session *models.TaskSession) context.Context {
	if _, captured := completionSignalFromContext(ctx, taskID, stepID); captured {
		return ctx
	}
	source := s.completionSignalForEntry(ctx, taskID, stepID, session)
	return models.WithStepCompletionSource(ctx, taskID, stepID, source)
}

func consumedCompletionSignal(source *completionSignalSource) *models.PendingStepCompletionSignal {
	if source == nil {
		return nil
	}
	return &source.Signal
}

func (s *Service) clearConsumedCompletionSignal(ctx context.Context, sessionID string, source *completionSignalSource) {
	if source != nil {
		s.clearPendingStepSignalValue(ctx, sessionID, source.Value)
	}
}
