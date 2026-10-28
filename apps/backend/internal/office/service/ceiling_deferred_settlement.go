package service

import (
	"context"
	"encoding/json"
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
	return s.settleCeilingDeferredRun(ctx, taskID, causingRunID, sessionID, turnID, "")
}

// settleCeilingDeferredRun is the shared exact-row settlement. taskID always
// scopes the update to the run's own task; agentProfileID adds the lifecycle
// event's agent identity when the ready-event recovery path supplies it.
func (s *Service) settleCeilingDeferredRun(
	ctx context.Context, taskID, causingRunID, sessionID, turnID, agentProfileID string,
) error {
	if causingRunID == "" {
		return nil
	}
	wrote, err := s.repo.SettleCeilingDeferredRun(ctx, causingRunID, sessionID, taskID, agentProfileID)
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

// settleCeilingDeferredLaunchOnReady is the event-driven recovery path for a
// ceiling replay whose synchronous settlement write failed. The replayed
// launch's ready event carries the same Office causing run id, so the parked
// row can be settled without a second launch. A normal task-owned launch is
// already claimed and this is a no-op.
func (s *Service) settleCeilingDeferredLaunchOnReady(
	ctx context.Context, data *AgentLifecycleData,
) error {
	if data == nil || data.TaskID == "" || data.CausingRunID == "" || data.SessionID == "" {
		return nil
	}
	deferred, err := s.isCeilingDeferredRun(ctx, data.CausingRunID)
	if err != nil {
		return err
	}
	if !deferred {
		return nil
	}
	return s.settleCeilingDeferredRun(
		ctx, data.TaskID, data.CausingRunID, data.SessionID, data.TurnID, data.AgentProfileID,
	)
}

// isCeilingDeferredRun reports whether the run's latest event is the durable
// ceiling-deferral marker. The ready recovery accepts only this marker, so a
// normal launch's ready event cannot release a run parked for another reason.
func (s *Service) isCeilingDeferredRun(ctx context.Context, runID string) (bool, error) {
	events, err := s.repo.ListRunEvents(ctx, runID, -1, 0)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return false, nil
	}
	latest := events[len(events)-1]
	if string(latest.EventType) != "adapter.invoke" {
		return false, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(latest.Payload), &payload); err != nil {
		return false, nil
	}
	return payload["phase"] == "deferred" && payload["reason"] == "session_ceiling", nil
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
	run, err := s.repo.FailCeilingDeferredRun(ctx, causingRunID, message, taskID)
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
