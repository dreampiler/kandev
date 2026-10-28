package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// CeilingDeferredLaunchReplayed settles the Office run whose deferred launch
// the session ceiling just admitted. The ceiling owns the launch; this only
// binds the resulting session to the run and releases the deferral park so the
// ordinary lifecycle events can finish it. It never launches or retries.
func (s *Service) CeilingDeferredLaunchReplayed(
	ctx context.Context, taskID, causingRunID, sessionID, turnID string,
) error {
	if causingRunID == "" {
		return nil
	}
	wrote, err := s.repo.SettleCeilingDeferredRun(ctx, causingRunID, sessionID)
	if err != nil {
		return fmt.Errorf("settle ceiling-deferred run %s: %w", causingRunID, err)
	}
	if !wrote {
		s.logger.Debug("ceiling replay: causing run is no longer parked",
			zap.String("run_id", causingRunID),
			zap.String("task_id", taskID))
		return nil
	}
	s.AppendRunEvent(ctx, causingRunID, "adapter.invoke", "info", map[string]interface{}{
		"phase":      "launch_reconciled",
		"reason":     "session_ceiling_replay",
		"task_id":    taskID,
		"session_id": sessionID,
		"turn_id":    turnID,
	})
	s.logger.Info("office run settled from ceiling replay",
		zap.String("run_id", causingRunID),
		zap.String("task_id", taskID),
		zap.String("session_id", sessionID))
	return nil
}

// CeilingDeferredLaunchDropped fails the Office run whose deferred launch the
// ceiling terminally dropped, since no session will ever be created for it.
// A run another path already settled is left untouched.
func (s *Service) CeilingDeferredLaunchDropped(
	ctx context.Context, taskID, causingRunID, reasonCode, detail string,
) error {
	if causingRunID == "" {
		return nil
	}
	message := fmt.Sprintf("the queued launch was dropped: %s", detail)
	run, err := s.repo.FailCeilingDeferredRun(ctx, causingRunID, message)
	if err != nil {
		return fmt.Errorf("fail ceiling-deferred run %s: %w", causingRunID, err)
	}
	if run == nil {
		s.logger.Debug("ceiling drop: causing run is no longer parked",
			zap.String("run_id", causingRunID),
			zap.String("task_id", taskID),
			zap.String("reason_code", reasonCode))
		return nil
	}
	s.AppendRunEvent(ctx, run.ID, "error", "error", map[string]interface{}{
		runEventFieldErrorMessage: message,
		"reason_code":             reasonCode,
		"task_id":                 taskID,
	})
	s.recordTerminalShape(ctx, run, RunStatusFailed, nil)
	s.publishRunProcessed(ctx, run.ID, RunStatusFailed, run)
	s.logger.Info("office run failed from ceiling drop",
		zap.String("run_id", run.ID),
		zap.String("task_id", taskID),
		zap.String("reason_code", reasonCode))
	return nil
}
