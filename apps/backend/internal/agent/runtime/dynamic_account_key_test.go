package runtime

import (
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
)

// Profiles with no credential of their own fall back to profile-scoped
// bindings; their account is the agent plus the model's provider, so OpenCode
// Zen and OpenCode Go are two accounts and every Zen profile shares one.
func TestCandidateAccountKeyGroupsFallbackBindingsByAgentAndProvider(t *testing.T) {
	opencode := &agentsettingsmodels.AgentProfile{AgentID: "agent-opencode"}
	zenA := candidateAccountKey(dynamic.ResourceKey(dynamic.ScopeCredential, "profile:zen-a"), opencode, "opencode/big-pickle")
	zenB := candidateAccountKey(dynamic.ResourceKey(dynamic.ScopeCredential, "profile:zen-b"), opencode, "opencode/space-bunny-free")
	goPaid := candidateAccountKey(dynamic.ResourceKey(dynamic.ScopeProfile, "go-glm"), opencode, "opencode-go/glm-5.3-flash")
	if zenA == "" || zenA != zenB {
		t.Fatalf("Zen profiles = %q and %q, want one shared account", zenA, zenB)
	}
	if goPaid == zenA {
		t.Fatalf("Go and Zen share account %q", goPaid)
	}
	credential := dynamic.ResourceKey(dynamic.ScopeCredential, "0123abcd")
	if got := candidateAccountKey(credential, opencode, "llmgateway/glm-5.3-flash"); got != credential {
		t.Fatalf("a real credential binding = %q, want the binding itself", got)
	}
	if got := candidateAccountKey(dynamic.ResourceKey(dynamic.ScopeCredential, "profile:x"), nil, "opencode/x"); got != "" {
		t.Fatalf("an unresolved profile = %q, want no account", got)
	}
}
