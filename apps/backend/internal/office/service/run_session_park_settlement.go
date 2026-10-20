package service

import (
	"context"
	"database/sql"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// Task session states read from task_session.state_changed. Office keeps the
// literals local because it does not import the task models package here.
const (
	taskSessionStateRunning         = "RUNNING"
	taskSessionStateWaitingForInput = "WAITING_FOR_INPUT"
)

// TaskSessionStateData is the subset of a task_session.state_changed payload
// that Office reads.
type TaskSessionStateData struct {
	TaskID         string `json:"task_id"`
	SessionID      string `json:"session_id"`
	OldState       string `json:"old_state"`
	NewState       string `json:"new_state"`
	ErrorMessage   string `json:"error_message"`
	AgentProfileID string `json:"agent_profile_id"`
}

// handleTaskSessionStateChanged settles a claimed task run whose launched
// session ended its turn on the per-launch session model. Such a session
// parks at WAITING_FOR_INPUT with its process still alive, so neither the
// Office IDLE teardown (AgentStopped) nor a process exit (AgentCompleted)
// ever reports the turn's end; the park is the only completion signal.
func (s *Service) handleTaskSessionStateChanged(ctx context.Context, event *bus.Event) error {
	data, err := decodeEventData[TaskSessionStateData](event)
	if err != nil || data == nil {
		return nil
	}
	s.resolvePendingAgentFailure(ctx, *data)
	if !isSuccessfulTurnPark(*data) {
		return nil
	}
	return s.settleParkedSessionRun(ctx, *data)
}

// isSuccessfulTurnPark reports a RUNNING -> WAITING_FOR_INPUT transition
// without an error. A failed turn parks with its error message and belongs to
// the failure path; an Office IDLE park is settled by its teardown, and so is
// an Office task's WAITING_FOR_INPUT park (see settleParkedSessionRun).
func isSuccessfulTurnPark(data TaskSessionStateData) bool {
	return data.TaskID != "" && data.SessionID != "" && data.AgentProfileID != "" &&
		data.OldState == taskSessionStateRunning &&
		data.NewState == taskSessionStateWaitingForInput &&
		data.ErrorMessage == ""
}

// receivesOfficeTeardown mirrors the orchestrator's handleOfficeTurnComplete
// predicate: an Office task whose session carries an agent profile (already
// required by isSuccessfulTurnPark) gets the IDLE teardown after its turn.
func (s *Service) receivesOfficeTeardown(ctx context.Context, taskID string) (bool, error) {
	fields, err := s.repo.GetTaskExecutionFields(ctx, taskID)
	if errors.Is(err, sqlite.ErrTaskNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return fields != nil && fields.IsFromOffice, nil
}

// settleParkedSessionRun finishes the agent's claimed run on the task only
// when that run launched the parked session, so a park on another session of
// the same task never settles a run it did not execute.
func (s *Service) settleParkedSessionRun(ctx context.Context, data TaskSessionStateData) error {
	run, err := s.repo.GetClaimedRunByTaskAndAgent(ctx, data.TaskID, data.AgentProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if run.SessionID == "" || run.SessionID != data.SessionID {
		return nil
	}
	officeTeardown, err := s.receivesOfficeTeardown(ctx, data.TaskID)
	if err != nil {
		return err
	}
	if officeTeardown {
		// The Office IDLE teardown settles this run through AgentStopped.
		// Settling here would free the agent's slot while that teardown is
		// still pending, so the next run could reuse the live execution and
		// then be stopped by the late teardown.
		return nil
	}
	s.logger.Info("settling Office run after its launched session parked",
		zap.String("run_id", run.ID),
		zap.String("task_id", data.TaskID),
		zap.String("session_id", data.SessionID))
	return s.completeClaimedTaskRun(ctx, run, AgentLifecycleData{
		TaskID:         data.TaskID,
		RunID:          run.ID,
		AgentProfileID: data.AgentProfileID,
		SessionID:      data.SessionID,
	})
}
