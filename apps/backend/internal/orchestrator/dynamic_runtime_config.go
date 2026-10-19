package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

type dynamicLaunchSnapshotUpdater interface {
	UpdateTaskSessionIfCurrentSnapshot(context.Context, *models.TaskSession, models.TaskSessionState, time.Time, map[string]interface{}) (bool, error)
}

const rejectedRuntimeSettingsExecution = "rejected_runtime_settings_execution"

// PersistDynamicRouteSelection shares the candidate reset boundary with manual
// route actions. The settings lock is released before their successor starts.
func (s *Service) PersistDynamicRouteSelection(ctx context.Context, observed, selected *models.TaskSession) (bool, error) {
	settingsLock := s.sessionSettingsSnapshotLock(selected.ID)
	settingsLock.Lock()
	defer settingsLock.Unlock()
	launching := selected.RouteState == "starting" || selected.RouteState == dynamicRouteStatusRetrying
	if launching && dynamicCandidateRuntimeResetRequired(observed, selected.ExecutionProfileID) {
		if selected.State == models.TaskSessionStateFailed {
			selected.State = models.TaskSessionStateCreated
			selected.ErrorMessage = ""
		}
		err := s.persistDynamicCandidateRuntimeReset(ctx, selected, observed.State, observed.UpdatedAt)
		if errors.Is(err, dynamicruntime.ErrStaleGeneration) {
			return false, nil
		}
		return err == nil, err
	}
	updater, ok := s.repo.(dynamicLaunchSnapshotUpdater)
	if !ok {
		return false, errors.New("dynamic route selection requires guarded metadata updates")
	}
	return updater.UpdateTaskSessionIfCurrentSnapshot(ctx, selected, observed.State, observed.UpdatedAt, selected.Metadata)
}

func dynamicCandidateRuntimeResetRequired(session *models.TaskSession, nextProfileID string) bool {
	if session.ExecutionProfileID == "" || nextProfileID == "" {
		return false
	}
	return session.ExecutionProfileID != nextProfileID || rejectedDynamicRuntimeExecution(session) != ""
}

// A typed, execution-bound pre-prompt rejection also permits discarding bad
// provider observations left by an older build. Explicit intent still wins.
func rejectedDynamicRuntimeExecution(session *models.TaskSession) string {
	failure, ok := models.LoadLastAgentError(session.Metadata)
	if !ok || failure.Phase != models.LaunchErrorPhaseBootstrap || session.AgentExecutionID == "" ||
		failure.ExecutionID != session.AgentExecutionID {
		return ""
	}
	for _, cause := range failure.Causes {
		if cause.Code == models.AgentErrorCauseCodePermissionModeFailed &&
			cause.Reason == models.AgentErrorCauseReasonApplicationFailed &&
			cause.PromptNotSent != nil && *cause.PromptNotSent {
			return session.AgentExecutionID
		}
	}
	return ""
}

// Called with the settings snapshot lock held. Only provider observations are
// reset; explicit user overrides and historical baseline metadata survive.
func (s *Service) persistDynamicCandidateRuntimeReset(
	ctx context.Context, session *models.TaskSession, expectedState models.TaskSessionState, expectedUpdatedAt time.Time,
) error {
	updater, ok := s.repo.(dynamicLaunchSnapshotUpdater)
	if !ok {
		return errors.New("dynamic candidate switch requires guarded metadata updates")
	}
	mode, err := s.dynamicCandidateWorkflowMode(ctx, session.TaskID)
	if err != nil {
		return err
	}
	session.Metadata = cloneSessionMetadata(session.Metadata)
	if rejected := rejectedDynamicRuntimeExecution(session); rejected != "" {
		session.Metadata[rejectedRuntimeSettingsExecution] = rejected
	}
	delete(session.Metadata, models.SessionMetaKeyRuntimeConfig)
	delete(session.Metadata, models.SessionMetaKeySessionMode)
	if mode != "" {
		session.Metadata[models.SessionMetaKeySessionMode] = mode
	}
	changed, err := updater.UpdateTaskSessionIfCurrentSnapshot(ctx, session, expectedState, expectedUpdatedAt, session.Metadata)
	if err != nil {
		return fmt.Errorf("persist dynamic candidate runtime reset: %w", err)
	}
	if !changed {
		return dynamicruntime.ErrStaleGeneration
	}
	return nil
}

func (s *Service) dynamicCandidateWorkflowMode(ctx context.Context, taskID string) (string, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("read dynamic candidate workflow: %w", err)
	}
	if task == nil {
		return "", errors.New("dynamic candidate task not found")
	}
	if task.WorkflowStepID == "" {
		return "", nil
	}
	if s.workflowStepGetter == nil {
		return "", errors.New("dynamic candidate workflow step reader unavailable")
	}
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil {
		return "", fmt.Errorf("read dynamic candidate workflow step: %w", err)
	}
	if step == nil {
		return "", errors.New("dynamic candidate workflow step not found")
	}
	var mode string
	for _, action := range step.Events.OnEnter {
		if action.Type == wfmodels.OnEnterSetSessionMode {
			if declared, ok := action.Config["mode"].(string); ok && declared != "" {
				mode = declared
			}
		}
	}
	return mode, nil
}
