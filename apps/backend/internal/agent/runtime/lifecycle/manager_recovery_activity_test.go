package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
)

// A terminal event re-applied from a retained, pre-restart turn outcome must
// not stamp lastActivityAt=now; otherwise the idle reaper restarts a session's
// idle interval from the backend restart instead of its prior real activity.
func TestCompleteEvent_RecoveredOutcomePreservesLastActivity(t *testing.T) {
	manager := newTestManager(t)
	execution := createTestExecution("exec-recovered-activity", "task-recovered", "session-recovered")
	require.NoError(t, manager.executionStore.Add(execution))
	generation, err := manager.executionStore.BeginPrompt(execution.ID)
	require.NoError(t, err)
	manager.executionStore.MarkPromptDispatched(execution.ID, generation)

	old := time.Now().Add(-3 * time.Hour)
	execution.lastActivityAtMu.Lock()
	execution.lastActivityAt = old
	execution.lastActivityAtMu.Unlock()

	require.True(t, manager.handleCompleteEvent(execution, &agentctl.AgentEvent{
		Type:             "complete",
		SessionID:        execution.SessionID,
		PromptGeneration: generation,
		Recovered:        true,
	}))

	lastActivity, _, _ := execution.promptActivitySnapshot()
	require.True(t, lastActivity.Equal(old),
		"recovered outcome must preserve lastActivityAt, got %v want %v", lastActivity, old)
}

// A live completion still advances lastActivityAt to the current time.
func TestCompleteEvent_LiveOutcomeStampsLastActivity(t *testing.T) {
	manager := newTestManager(t)
	execution := createTestExecution("exec-live-activity", "task-live", "session-live")
	require.NoError(t, manager.executionStore.Add(execution))
	generation, err := manager.executionStore.BeginPrompt(execution.ID)
	require.NoError(t, err)
	manager.executionStore.MarkPromptDispatched(execution.ID, generation)

	old := time.Now().Add(-3 * time.Hour)
	execution.lastActivityAtMu.Lock()
	execution.lastActivityAt = old
	execution.lastActivityAtMu.Unlock()

	require.True(t, manager.handleCompleteEvent(execution, &agentctl.AgentEvent{
		Type:             "complete",
		SessionID:        execution.SessionID,
		PromptGeneration: generation,
	}))

	lastActivity, _, _ := execution.promptActivitySnapshot()
	require.WithinDuration(t, time.Now(), lastActivity, time.Minute,
		"live completion must stamp lastActivityAt, got %v", lastActivity)
}
