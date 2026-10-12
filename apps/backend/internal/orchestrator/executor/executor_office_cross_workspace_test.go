package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	mcpprofile "github.com/kandev/kandev/internal/mcp/profile"
	"github.com/kandev/kandev/internal/task/models"
)

type fakeOfficeCrossWorkspaceLookup struct {
	enabled   map[string]bool
	receivers map[string]bool
}

func (f fakeOfficeCrossWorkspaceLookup) Enabled(_ context.Context, ws string) bool {
	return f.enabled[ws]
}

func (f fakeOfficeCrossWorkspaceLookup) ReceivesHandoffReports(_ context.Context, ws string) bool {
	return f.receivers[ws]
}

func TestResolveTaskSessionMCPProfile_OfficeCrossWorkspaceCapabilities(t *testing.T) {
	lookup := fakeOfficeCrossWorkspaceLookup{
		enabled:   map[string]bool{"office-on": true},
		receivers: map[string]bool{"kanban-reached": true},
	}
	tests := []struct {
		name       string
		task       *models.Task
		lookup     OfficeCrossWorkspaceLookup
		wantCross  bool
		wantReport bool
	}{
		{name: "office task, setting on", task: &models.Task{ID: "t1", WorkspaceID: "office-on", IsFromOffice: true}, lookup: lookup, wantCross: true},
		{name: "office task, setting off", task: &models.Task{ID: "t2", WorkspaceID: "office-off", IsFromOffice: true}, lookup: lookup},
		{name: "kanban task reached by an office", task: &models.Task{ID: "t3", WorkspaceID: "kanban-reached"}, lookup: lookup, wantReport: true},
		{name: "kanban task not reached", task: &models.Task{ID: "t4", WorkspaceID: "kanban-alone"}, lookup: lookup},
		{name: "no lookup wired", task: &models.Task{ID: "t5", WorkspaceID: "office-on", IsFromOffice: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			repo.tasks[tt.task.ID] = tt.task
			session := &models.TaskSession{ID: "session-" + tt.task.ID, TaskID: tt.task.ID}
			repo.sessions[session.ID] = session
			exec := newTestExecutor(t, &mockAgentManager{}, repo)
			if tt.lookup != nil {
				exec.SetOfficeCrossWorkspaceLookup(tt.lookup)
			}
			profile, err := exec.resolveTaskSessionMCPProfile(context.Background(), tt.task.ID, session, false)
			require.NoError(t, err)
			require.Equal(t, tt.wantCross, profile.HasCapability(mcpprofile.CapabilityOfficeCrossWorkspace))
			require.Equal(t, tt.wantReport, profile.HasCapability(mcpprofile.CapabilityOfficeHandoffReport))
		})
	}
}
