package orchestrator

import (
	"context"
	"testing"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/lifecycle"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

const (
	previousCandidateProfileID = "candidate-previous"
	nextCandidateProfileID     = "candidate-next"
)

// seedDynamicCandidateRuntimeSettings records what the previous concrete
// provider reported (model and permission mode) plus an explicit user choice.
func seedDynamicCandidateRuntimeSettings(t *testing.T, repo *sqliterepo.Repository, sessionID string) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	session.ExecutionProfileID = previousCandidateProfileID
	session.RouteGeneration = 1
	session.RouteState = "running"
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatalf("update session: %v", err)
	}
	for key, value := range map[string]interface{}{
		models.SessionMetaKeyRuntimeConfig:          models.SessionRuntimeConfig{Model: "previous-model", Mode: "previous-mode"},
		models.SessionMetaKeySessionMode:            "previous-mode",
		models.SessionMetaKeyRuntimeConfigOverrides: models.SessionRuntimeConfig{ConfigOptions: map[string]string{"effort": "high"}},
	} {
		if err := repo.SetSessionMetadataKey(ctx, sessionID, key, value); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
}

// The models handler persists the current model after its staleness check, so
// a late models event from the previous candidate must be dropped there too.
func TestSessionModelsEventFromPreviousCandidateIsIgnoredAfterSwitch(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "task-candidate-late-models", "session-candidate-late-models", models.TaskSessionStateFailed)
	seedDynamicCandidateRuntimeSettings(t, repo, "session-candidate-late-models")
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	eventBus := &recordingEventBus{}
	svc.eventBus = eventBus

	if err := svc.persistDynamicLaunchDecision(ctx, "session-candidate-late-models", dynamicruntime.RouteDecision{
		ExecutionProfileID: nextCandidateProfileID,
		Generation:         2,
	}); err != nil {
		t.Fatalf("persist candidate switch: %v", err)
	}
	svc.handleSessionModelsEvent(ctx, &lifecycle.AgentStreamEventPayload{
		TaskID:             "task-candidate-late-models",
		SessionID:          "session-candidate-late-models",
		ExecutionID:        "previous-execution",
		ExecutionProfileID: previousCandidateProfileID,
		Data:               &lifecycle.AgentStreamEventData{CurrentModelID: "previous-model"},
	})

	session, err := repo.GetTaskSession(ctx, "session-candidate-late-models")
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	effective, _ := models.LoadEffectiveSessionRuntimeConfig(session)
	if effective.Model == "previous-model" {
		t.Fatalf("late models event from the previous candidate restored its model: %#v", session.Metadata)
	}
	if len(eventBus.events) != 0 {
		t.Fatalf("late models event from the previous candidate was published: %d events", len(eventBus.events))
	}
}
