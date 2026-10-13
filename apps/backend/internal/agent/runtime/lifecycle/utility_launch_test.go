package lifecycle

import (
	"context"
	"reflect"
	"testing"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/managedruntime"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/agentruntime"
	"github.com/kandev/kandev/internal/secrets"
)

func TestInferenceReviewLaunchResolvesProfileSecret(t *testing.T) {
	profile := &AgentProfileInfo{AgentID: "claude-review", EnvVars: []settingsmodels.ProfileEnvVar{
		{Key: "CLAUDE_CODE_OAUTH_TOKEN", SecretID: "review-token"},
	}}
	h := newInferenceHarness(t, &stubProfileResolver{profile: profile}, newInferenceAgent("claude-review", true, true))
	h.manager.secretStore = newInMemorySecretStore()
	if err := h.manager.secretStore.Create(context.Background(), &secrets.SecretWithValue{
		Secret: secrets.Secret{ID: "review-token", Name: "review-token"}, Value: "synthetic-token",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := h.manager.ExecuteInferenceProfilePrompt(t.Context(), "session-1", "review", "review this")
	if err != nil {
		t.Fatal(err)
	}
	if h.recorded()[0].InferenceConfig.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "synthetic-token" {
		t.Fatal("review launch omitted CLAUDE_CODE_OAUTH_TOKEN")
	}
}

func TestInferenceReviewLaunchUsesSessionCommandAndRuntimeEnv(t *testing.T) {
	ag := agents.NewOpenCodeACP()
	h := newInferenceHarness(t, &stubProfileResolver{profile: &AgentProfileInfo{AgentID: ag.ID()}}, ag)
	h.manager.SetManagedRuntimeSelectionStore(managedRuntimeOpenCodeStore{openCodeSelection: managedruntime.OpenCodeSelection{
		Family: managedruntime.OpenCodeFamilyV2, Source: managedruntime.OpenCodeSourceManaged, SelectedVersion: "2.0.3",
	}})
	execution, _ := h.manager.executionStore.GetBySessionID("session-1")
	execution.RuntimeName = agentruntime.RuntimeStandalone
	_, err := h.manager.ExecuteInferenceProfilePrompt(t.Context(), "session-1", "review", "review this")
	if err != nil {
		t.Fatal(err)
	}
	commands, err := h.manager.buildAgentCommandWithContext(t.Context(), &LaunchRequest{}, nil, ag,
		execution.RuntimeName, h.manager.preferNativeBinary(ag, execution.RuntimeName, nil))
	if err != nil {
		t.Fatal(err)
	}
	got := h.recorded()[0].InferenceConfig
	if !reflect.DeepEqual(got.Command, commands.args) {
		t.Fatalf("review command differs from session launch: got %v, want %v", got.Command, commands.args)
	}
	for key, want := range ag.Runtime().Env {
		if got.Env[key] != want {
			t.Fatalf("review launch omitted runtime env key %s", key)
		}
	}
}

func TestInferenceReviewLaunchIncludesRuntimeDefaultsAndCredentials(t *testing.T) {
	ag := newInferenceAgent("review-env", true, true)
	ag.runtimeEnv = map[string]string{"RUNTIME_DEFAULT": "synthetic-default"}
	ag.requiredEnv = []string{"REQUIRED_CREDENTIAL"}
	h := newInferenceHarness(t, &stubProfileResolver{profile: &AgentProfileInfo{AgentID: ag.ID()}}, ag)
	h.manager.credsMgr = fixedCredentialManager{value: "synthetic-credential"}
	_, err := h.manager.ExecuteInferenceProfilePrompt(t.Context(), "session-1", "review", "review this")
	if err != nil {
		t.Fatal(err)
	}
	env := h.recorded()[0].InferenceConfig.Env
	if env["RUNTIME_DEFAULT"] != "synthetic-default" || env["REQUIRED_CREDENTIAL"] != "synthetic-credential" {
		t.Fatal("review omitted runtime defaults or required credentials")
	}
}

func TestInferenceReviewLaunchMissingSecretFailsBeforeDispatch(t *testing.T) {
	h := newInferenceHarness(t, &stubProfileResolver{profile: &AgentProfileInfo{
		AgentID: "review-env", EnvVars: []settingsmodels.ProfileEnvVar{{Key: "TOKEN", SecretID: "missing", Value: "must-not-fallback"}},
	}}, newInferenceAgent("review-env", true, true))
	_, err := h.manager.ExecuteInferenceProfilePrompt(t.Context(), "session-1", "review", "review this")
	if err == nil || len(h.recorded()) != 0 {
		t.Fatal("missing secret must fail before dispatch")
	}
}
