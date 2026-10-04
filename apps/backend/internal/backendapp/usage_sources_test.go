package backendapp

import (
	"testing"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

func testBindingResolver(t *testing.T, env map[string]string) *usageBindingResolver {
	t.Helper()
	return &usageBindingResolver{home: t.TempDir(), getenv: func(key string) string { return env[key] }}
}

func TestOpenCodeModelPrefixSelectsTheProviderAccount(t *testing.T) {
	resolver := testBindingResolver(t, nil)
	cases := []struct {
		model      string
		wantClient bool
		wantKind   string
	}{
		{"opencode-go/glm-5.3", true, "opencode-go"},
		{"opencode-go/longcat-2.5-preview-free", false, "opencode-go-free"},
		{"opencode/ling-3.0-flash-fin-free", false, "opencode-zen"},
		{"openrouter/nvidia/nemotron-3-ultra-550b-a55b:free", true, "openrouter"},
		{"llmgateway/deepseek-v4-pro", true, "llmgateway"},
	}
	for _, tc := range cases {
		binding, ok := resolver.Resolve(&settingsmodels.AgentProfile{Model: tc.model}, openCodeACPAgentID)
		if !ok {
			t.Fatalf("%s: no binding", tc.model)
		}
		if (binding.client != nil) != tc.wantClient || binding.accountKind() != tc.wantKind {
			t.Fatalf("%s: client=%v kind=%q, want client=%v kind=%q",
				tc.model, binding.client != nil, binding.accountKind(), tc.wantClient, tc.wantKind)
		}
	}
	if _, ok := resolver.Resolve(&settingsmodels.AgentProfile{Model: "modal/kimi"}, openCodeACPAgentID); ok {
		t.Fatal("a provider without a usage source must not be bound")
	}
}

func TestClaudeCredentialPrecedence(t *testing.T) {
	processEnv := map[string]string{agentusage.ClaudeOAuthTokenEnv: "process-token"}
	withProfileToken := &settingsmodels.AgentProfile{
		ID: "p1", EnvVars: []settingsmodels.ProfileEnvVar{{Key: agentusage.ClaudeOAuthTokenEnv, Value: "profile-token"}},
	}
	binding, _ := testBindingResolver(t, processEnv).Resolve(withProfileToken, claudeACPAgentID)
	if binding.accountKey != "anthropic:profile-env:p1" {
		t.Fatalf("account = %q, want the profile's own token first", binding.accountKey)
	}
	binding, _ = testBindingResolver(t, processEnv).Resolve(&settingsmodels.AgentProfile{ID: "p2"}, claudeACPAgentID)
	if binding.accountKey != claudeProcessEnvAccount {
		t.Fatalf("account = %q, want the inherited process token", binding.accountKey)
	}
	binding, _ = testBindingResolver(t, nil).Resolve(&settingsmodels.AgentProfile{ID: "p3"}, claudeACPAgentID)
	if binding.accountKind() != "anthropic" || binding.accountKey == claudeProcessEnvAccount {
		t.Fatalf("account = %q, want the CLI credentials file", binding.accountKey)
	}
	secretOnly := &settingsmodels.AgentProfile{
		ID: "p4", EnvVars: []settingsmodels.ProfileEnvVar{{Key: agentusage.ClaudeOAuthTokenEnv, SecretID: "secret"}},
	}
	binding, _ = testBindingResolver(t, processEnv).Resolve(secretOnly, claudeACPAgentID)
	if binding.accountKey != claudeProcessEnvAccount {
		t.Fatalf("account = %q, want the process token when no secret store is configured", binding.accountKey)
	}
}
