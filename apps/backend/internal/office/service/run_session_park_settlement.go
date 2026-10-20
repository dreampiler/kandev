package service

import (
	"context"
	"database/sql"
	"errors"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/events/bus"
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
// session ended its turn. A session on the per-launch model has no IDLE
// teardown, and an Office turn depends on the park whenever its IDLE teardown
// does not stop the agent — a cancelled turn, a session without an agent
// execution, or a session relaunched for the next run. In all of those neither
// AgentStopped nor AgentCompleted reports the turn's end, so the park is the
// one completion signal every turn produces.
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
// the failure path. Every other park settles the run: a Routine-workflow
// session has no IDLE teardown, and an Office turn depends on this park too
// whenever its IDLE teardown does not stop the agent — a cancelled turn, a
// session without an agent execution, or a session relaunched for the next
// run. The park is the one signal every turn produces, so it is the settlement
// point; the orchestrator keeps the IDLE teardown from stopping an agent a
// relaunched session no longer owns.
func isSuccessfulTurnPark(data TaskSessionStateData) bool {
	return data.TaskID != "" && data.SessionID != "" && data.AgentProfileID != "" &&
		data.OldState == taskSessionStateRunning &&
		data.NewState == taskSessionStateWaitingForInput &&
		data.ErrorMessage == ""
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
