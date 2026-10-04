package backendapp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	"github.com/kandev/kandev/internal/secrets"
)

// usageSecretStore is a scoped secret store double holding fixed values.
type usageSecretStore struct {
	values map[string]string
	scopes map[string]secrets.SecretScope
}

var _ secrets.ScopedSecretStore = (*usageSecretStore)(nil)

func newUsageSecretStore(values map[string]string, scopes map[string]secrets.SecretScope) *usageSecretStore {
	return &usageSecretStore{values: values, scopes: scopes}
}

func (s *usageSecretStore) Create(context.Context, *secrets.SecretWithValue) error { return nil }

func (s *usageSecretStore) Get(_ context.Context, id string) (*secrets.Secret, error) {
	if _, ok := s.values[id]; !ok {
		return nil, fmt.Errorf("%w: %s", secrets.ErrNotFound, id)
	}
	return &secrets.Secret{ID: id, Scope: s.scope(id)}, nil
}

func (s *usageSecretStore) Reveal(_ context.Context, id string) (string, error) {
	value, ok := s.values[id]
	if !ok {
		return "", fmt.Errorf("%w: %s", secrets.ErrNotFound, id)
	}
	return value, nil
}

func (s *usageSecretStore) Update(context.Context, string, *secrets.UpdateSecretRequest) error {
	return nil
}

func (s *usageSecretStore) Delete(context.Context, string) error { return nil }

func (s *usageSecretStore) List(context.Context) ([]*secrets.SecretListItem, error) {
	return nil, nil
}

func (s *usageSecretStore) Close() error { return nil }

func (s *usageSecretStore) ListScoped(context.Context, secrets.SecretListOptions) ([]*secrets.SecretListItem, error) {
	return nil, nil
}

func (s *usageSecretStore) GetForWorkspace(ctx context.Context, id, workspaceID string) (*secrets.Secret, error) {
	secret, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if secret.Scope != secrets.ScopeGlobal && secret.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("%w: %s", secrets.ErrNotFound, id)
	}
	return secret, nil
}

func (s *usageSecretStore) RevealGlobal(ctx context.Context, id string) (string, error) {
	if s.scope(id) != secrets.ScopeGlobal {
		return "", fmt.Errorf("%w: %s", secrets.ErrNotFound, id)
	}
	return s.Reveal(ctx, id)
}

func (s *usageSecretStore) RevealForWorkspace(ctx context.Context, id, workspaceID string) (string, error) {
	if _, err := s.GetForWorkspace(ctx, id, workspaceID); err != nil {
		return "", err
	}
	return s.Reveal(ctx, id)
}

func (s *usageSecretStore) DeleteWorkspaceSecrets(context.Context, string) error { return nil }

func (s *usageSecretStore) scope(id string) secrets.SecretScope {
	if scope, ok := s.scopes[id]; ok {
		return scope
	}
	return secrets.ScopeGlobal
}

func testBindingResolverWithStore(t *testing.T, env map[string]string, store secrets.SecretStore) *usageBindingResolver {
	t.Helper()
	return &usageBindingResolver{
		home:        t.TempDir(),
		getenv:      func(key string) string { return env[key] },
		secretStore: store,
	}
}

func claudeProfileWithToken(id string, env settingsmodels.ProfileEnvVar) *settingsmodels.AgentProfile {
	return &settingsmodels.AgentProfile{ID: id, EnvVars: []settingsmodels.ProfileEnvVar{env}}
}

func secretTokenProfile(id string, secretID string) *settingsmodels.AgentProfile {
	return claudeProfileWithToken(id, settingsmodels.ProfileEnvVar{
		Key: agentusage.ClaudeOAuthTokenEnv, SecretID: secretID,
	})
}

// A Claude profile authenticates with a secret reference, so its usage is read
// from that account rather than from the process token or the CLI credentials
// file, and profiles sharing the reference share one cached read.
func TestClaudeUsageReadsTheProfileSecretReference(t *testing.T) {
	store := newUsageSecretStore(
		map[string]string{"secret-shared": "oauth-token"},
		map[string]secrets.SecretScope{"secret-shared": secrets.ScopeGlobal},
	)
	resolver := testBindingResolverWithStore(
		t,
		map[string]string{agentusage.ClaudeOAuthTokenEnv: "process-token"},
		store,
	)

	first, ok := resolver.Resolve(secretTokenProfile("p1", "secret-shared"), claudeACPAgentID)
	if !ok {
		t.Fatal("a Claude profile must resolve to an account")
	}
	wantKey := claudeProfileSecretAccountPrefix + "secret-shared"
	if first.accountKey != wantKey {
		t.Fatalf("account = %q, want the referenced secret account %q", first.accountKey, wantKey)
	}
	if first.cacheKey != agentusage.CacheKey("anthropic", wantKey) {
		t.Fatalf("cache key = %q, want one shared key for the account", first.cacheKey)
	}

	second, _ := resolver.Resolve(secretTokenProfile("p2", "secret-shared"), claudeACPAgentID)
	if second.cacheKey != first.cacheKey {
		t.Fatalf("cache keys differ for one secret: %q vs %q", first.cacheKey, second.cacheKey)
	}
}

// A secret reference is the credential the agent itself runs with, so it
// outranks a literal value stored beside it and an inherited process token.
func TestClaudeUsagePrefersTheSecretReferenceOverLiteralAndProcessToken(t *testing.T) {
	store := newUsageSecretStore(
		map[string]string{"secret-ref": "oauth-token"},
		map[string]secrets.SecretScope{"secret-ref": secrets.ScopeGlobal},
	)
	resolver := testBindingResolverWithStore(
		t,
		map[string]string{agentusage.ClaudeOAuthTokenEnv: "process-token"},
		store,
	)

	profile := claudeProfileWithToken("p1", settingsmodels.ProfileEnvVar{
		Key: agentusage.ClaudeOAuthTokenEnv, Value: "literal-token", SecretID: "secret-ref",
	})
	binding, _ := resolver.Resolve(profile, claudeACPAgentID)
	if binding.accountKey != claudeProfileSecretAccountPrefix+"secret-ref" {
		t.Fatalf("account = %q, want the secret reference the agent resolves", binding.accountKey)
	}
}

// A profile with no credential of its own keeps reading the inherited process
// token, and the resolver returns the stored value for a global reference.
func TestClaudeUsageFallsBackWithoutACredentialOfItsOwn(t *testing.T) {
	store := newUsageSecretStore(map[string]string{"secret-ref": "oauth-token"}, nil)
	resolver := testBindingResolverWithStore(
		t,
		map[string]string{agentusage.ClaudeOAuthTokenEnv: "process-token"},
		store,
	)

	binding, _ := resolver.Resolve(&settingsmodels.AgentProfile{ID: "p1"}, claudeACPAgentID)
	if binding.accountKey != claudeProcessEnvAccount {
		t.Fatalf("account = %q, want the inherited process token", binding.accountKey)
	}

	token, err := resolver.claudeSecretToken("secret-ref")(context.Background())
	if err != nil || token != "oauth-token" {
		t.Fatalf("token = %q err = %v, want the stored global secret", token, err)
	}
}

// A reference that cannot be revealed is a bounded failure that carries
// neither the secret id nor the store error, because the account key and the
// failure log both leave this path.
func TestClaudeUsageSecretFailureIsSanitized(t *testing.T) {
	resolver := testBindingResolverWithStore(t, nil, newUsageSecretStore(map[string]string{}, nil))

	token, err := resolver.claudeSecretToken("secret-gone")(context.Background())
	if err == nil {
		t.Fatal("a missing secret must fail the read")
	}
	if token != "" {
		t.Fatalf("token = %q, want no value on failure", token)
	}
	if strings.Contains(err.Error(), "secret-gone") {
		t.Fatalf("error %v must not name the secret", err)
	}
}

// A secret outside the global scope is not an approved source for an agent
// profile environment, so it must not resolve a usage read either.
func TestClaudeUsageRefusesANonGlobalSecret(t *testing.T) {
	store := newUsageSecretStore(
		map[string]string{"secret-ws": "oauth-token"},
		map[string]secrets.SecretScope{"secret-ws": secrets.ScopeWorkspace},
	)
	resolver := testBindingResolverWithStore(t, nil, store)

	if _, err := resolver.claudeSecretToken("secret-ws")(context.Background()); err == nil {
		t.Fatal("a workspace-scoped secret must not resolve a usage read")
	}
}
