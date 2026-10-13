package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/plugins/manifest"
	"github.com/stretchr/testify/require"
)

type restartCatalogListener struct {
	svc        *Service
	t          *testing.T
	toolCounts []int
}

func (l *restartCatalogListener) NotifyAgentToolCatalogChanged() {
	snapshot, err := l.svc.AgentToolCatalog()
	require.NoError(l.t, err)
	l.toolCounts = append(l.toolCounts, len(snapshot.Tools))
}

func TestUpdateConfigRestartFailureRefreshesAgentTools(t *testing.T) {
	svc, fsStore, rt := newTestService(t)
	svc.SetSecrets(newFakeSecretRevealer())
	rec := installConfigPlugin(t, svc, "kandev-plugin-github")
	rec.AgentTools = []manifest.AgentTool{{
		Name: "echo", Description: "Echo input.",
		Surfaces: []string{manifest.AgentToolSurfaceKanban}, InputSchema: map[string]any{"type": "object"},
	}}
	require.NoError(t, fsStore.Save(rec))
	svc.registry.Add(rec)
	require.NoError(t, svc.UpdateConfig(context.Background(), rec.ID, map[string]any{"github_token": "test-token"}))
	listener := &restartCatalogListener{svc: svc, t: t}
	svc.SetAgentToolCatalogListener(listener)
	listener.NotifyAgentToolCatalogChanged()
	require.Equal(t, []int{1}, listener.toolCounts)
	rt.setStartErr(rec.ID, errors.New("spawn failed"))
	require.Error(t, svc.UpdateConfig(context.Background(), rec.ID, map[string]any{"github_token": "replacement-token"}))
	require.NotEmpty(t, listener.toolCounts)
	require.Zero(t, listener.toolCounts[len(listener.toolCounts)-1], "last catalog notification must remove errored plugin tools")
	failed, err := svc.Get(rec.ID)
	require.NoError(t, err)
	require.Equal(t, StatusError, failed.Status)
}
