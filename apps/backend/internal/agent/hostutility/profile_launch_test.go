package hostutility

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/registry"
	agentctlclient "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	utility "github.com/kandev/kandev/internal/agentctl/server/utility"
	"github.com/stretchr/testify/require"
)

func TestHostProfilePromptUsesSessionLaunchResolvers(t *testing.T) {
	log := newTestLogger(t)
	reg := registry.NewRegistry(log)
	require.NoError(t, reg.Register(&installedInferenceAgent{id: "claude-review"}))
	requests := make(chan utility.PromptRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		var req utility.PromptRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- req
		_ = json.NewEncoder(w).Encode(utility.PromptResponse{Success: true, Response: "ok"})
	}))
	t.Cleanup(server.Close)
	host, port := serverHostPort(t, server)
	m := NewManager(reg, host, port, nil, log)
	m.SetProfileResolver(profilePromptResolver{profile: &settingsmodels.AgentProfile{
		AgentID: "claude-review", EnvVars: []settingsmodels.ProfileEnvVar{{Key: "TOKEN", SecretID: "saved-secret"}},
	}})
	called := false
	m.SetUtilityLaunchResolvers(func(_ context.Context, agentID string, vars []settingsmodels.ProfileEnvVar) (map[string]string, error) {
		called = agentID == "claude-review" && len(vars) == 1 && vars[0].SecretID == "saved-secret"
		return map[string]string{"TOKEN": "synthetic-secret", "RUNTIME_DEFAULT": "synthetic-default"}, nil
	}, func(context.Context, string) (agents.Command, error) {
		return agents.NewCommand("selected-runtime", "acp"), nil
	})
	workdir := t.TempDir()
	m.instances["claude-review"] = &instance{agentType: "claude-review", workDir: workdir, client: agentctlclient.NewClient(host, port, log)}
	_, err := m.ExecuteProfilePrompt(t.Context(), "review-profile", "review")
	require.NoError(t, err)
	require.True(t, called)
	req := <-requests
	require.Equal(t, []string{"selected-runtime", "acp"}, req.InferenceConfig.Command)
	if req.InferenceConfig.Env["TOKEN"] != "synthetic-secret" || req.InferenceConfig.Env["RUNTIME_DEFAULT"] != "synthetic-default" {
		t.Fatal("host prompt omitted resolved environment")
	}
	require.Equal(t, workdir, req.InferenceConfig.WorkDir)
}
