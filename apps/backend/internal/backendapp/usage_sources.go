package backendapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	"github.com/kandev/kandev/internal/secrets"
)

const (
	codexACPAgentID       = "codex-acp"
	codexAppServerAgentID = "codex-app-server"
	antigravityAgentID    = "antigravity-acp"
	openCodeACPAgentID    = "opencode-acp"

	// Usage source labels returned to clients. They name where a reading came
	// from, never the credential itself.
	usageSourceProviderAPI = "provider_api"
	usageSourceProxy       = "proxy"
	usageSourceNone        = "none"
)

// usageBinding is the account reader for one concrete profile: which client
// reads the account, the cache key that lets profiles on the same account share
// one read, and how the profile's model is classified against class-scoped
// windows. A binding without a client is an account Kandev can group but whose
// provider publishes no usage API.
type usageBinding struct {
	client     agentusage.ProviderUsageClient
	cacheKey   string
	source     string
	accountKey string
	modelID    string
	class      modelClassResolver
}

// modelClassResolver reports the provider's class for the profile's model.
type modelClassResolver func(ctx context.Context) agentusage.ModelClass

func fixedModelClass(class agentusage.ModelClass) modelClassResolver {
	return func(context.Context) agentusage.ModelClass { return class }
}

func catalogModelClass(catalog agentusage.ModelClassifier, modelID string) modelClassResolver {
	return func(ctx context.Context) agentusage.ModelClass { return catalog.ClassifyModel(ctx, modelID) }
}

// usageBindingResolver maps a concrete profile to its own account. It only
// reads credentials a host-run agent of that type actually uses, so a profile
// is never answered with another account's usage.
type usageBindingResolver struct {
	home   string
	getenv func(string) string
	// secretStore reveals a token the profile references instead of carrying.
	// It is the same source the launch path resolves the agent environment
	// from, so a profile's usage is read from the account its agent uses.
	secretStore secrets.SecretStore
	// openRouterDailyLimit is the configured free-model daily allowance.
	openRouterDailyLimit int

	catalogOnce       sync.Once
	openRouterCatalog agentusage.ModelClassifier
	llmGatewayCatalog agentusage.ModelClassifier
}

func newUsageBindingResolver(openRouterDailyLimit int, secretStore secrets.SecretStore) *usageBindingResolver {
	home, _ := os.UserHomeDir()
	return &usageBindingResolver{
		home:                 home,
		getenv:               os.Getenv,
		secretStore:          secretStore,
		openRouterDailyLimit: openRouterDailyLimit,
	}
}

func (r *usageBindingResolver) catalogs() (agentusage.ModelClassifier, agentusage.ModelClassifier) {
	r.catalogOnce.Do(func() {
		if r.openRouterCatalog == nil {
			r.openRouterCatalog = agentusage.NewOpenRouterModelCatalog()
		}
		if r.llmGatewayCatalog == nil {
			r.llmGatewayCatalog = agentusage.NewLLMGatewayModelCatalog(agentusage.OpenCodeAuthPath(r.home))
		}
	})
	return r.openRouterCatalog, r.llmGatewayCatalog
}

// Resolve returns the profile's account reader for an agent of agentType. The
// second result is false when Kandev has no way to identify the account.
func (r *usageBindingResolver) Resolve(
	profile *settingsmodels.AgentProfile,
	agentType string,
) (usageBinding, bool) {
	if profile == nil || r.home == "" {
		return usageBinding{}, false
	}
	model := strings.TrimSpace(profile.Model)
	switch agentType {
	case claudeACPAgentID:
		return r.claudeBinding(profile, model), true
	case codexACPAgentID, codexAppServerAgentID:
		authPath := filepath.Join(r.home, ".codex", "auth.json")
		return usageBinding{
			client:   agentusage.NewCodexUsageClientWithPath(authPath),
			cacheKey: agentusage.CacheKey("openai", authPath), source: usageSourceProviderAPI,
			accountKey: "openai:" + authPath, modelID: model, class: fixedModelClass(agentusage.ModelClassUnknown),
		}, true
	case antigravityAgentID:
		credPath := filepath.Join(r.home, ".gemini", "antigravity-cli")
		return usageBinding{
			client:   agentusage.NewAntigravityUsageClient(),
			cacheKey: agentusage.CacheKey("google-antigravity", credPath), source: usageSourceProviderAPI,
			accountKey: "google-antigravity:" + credPath, modelID: model, class: fixedModelClass(agentusage.ModelClassUnknown),
		}, true
	case openCodeACPAgentID:
		return r.openCodeBinding(model)
	default:
		return usageBinding{}, false
	}
}

// claudeProcessEnvAccount groups profiles that inherit the Kandev process token.
const claudeProcessEnvAccount = "anthropic:process-env"

// claudeProfileSecretAccountPrefix groups profiles whose Claude OAuth token is
// one secret-store reference, so they share a single cached read.
const claudeProfileSecretAccountPrefix = "anthropic:profile-secret:"

// errClaudeTokenUnavailable replaces every secret-store failure on this path.
// A store error names the secret it could not reveal, and the account key must
// stay safe to log, so neither the id nor the store error is carried onward.
var errClaudeTokenUnavailable = errors.New("claude oauth token unavailable")

// claudeBinding follows the credential precedence a host-run Claude agent sees:
// a token the profile's own environment references or carries, then one
// inherited from the Kandev process, then the CLI credentials file.
func (r *usageBindingResolver) claudeBinding(profile *settingsmodels.AgentProfile, model string) usageBinding {
	binding := usageBinding{source: usageSourceProviderAPI, modelID: model, class: fixedModelClass(agentusage.ModelClassUnknown)}
	if secretID := profileEnvSecretID(profile, agentusage.ClaudeOAuthTokenEnv); secretID != "" && r.secretStore != nil {
		accountKey := claudeProfileSecretAccountPrefix + secretID
		binding.client = agentusage.NewClaudeUsageClientWithTokenResolver(r.claudeSecretToken(secretID))
		binding.cacheKey = agentusage.CacheKey("anthropic", accountKey)
		binding.accountKey = accountKey
		return binding
	}
	if token := profileEnvLiteral(profile, agentusage.ClaudeOAuthTokenEnv); token != "" {
		binding.client = agentusage.NewClaudeUsageClientWithOAuthToken(token)
		binding.cacheKey = agentusage.CacheKey("anthropic", "profile-env:"+profile.ID)
		binding.accountKey = "anthropic:profile-env:" + profile.ID
		return binding
	}
	if token := strings.TrimSpace(r.getenv(agentusage.ClaudeOAuthTokenEnv)); token != "" {
		binding.client = agentusage.NewClaudeUsageClientWithOAuthToken(token)
		binding.cacheKey = agentusage.CacheKey("anthropic", "process-env")
		binding.accountKey = claudeProcessEnvAccount
		return binding
	}
	credPath := filepath.Join(r.home, ".claude", ".credentials.json")
	binding.client = agentusage.NewClaudeUsageClientWithPath(credPath)
	binding.cacheKey = agentusage.CacheKey("anthropic", credPath)
	binding.accountKey = "anthropic:" + credPath
	return binding
}

// claudeSecretToken reads the profile's token from the secret store at read
// time, with the same global-scope rule the launch path applies to agent
// profile environment. The value is handed straight to the provider request:
// it is never stored, logged, or returned to a client.
func (r *usageBindingResolver) claudeSecretToken(secretID string) agentusage.TokenResolver {
	return func(ctx context.Context) (string, error) {
		if scoped, ok := r.secretStore.(secrets.ScopedSecretStore); ok {
			token, err := scoped.RevealGlobal(ctx, secretID)
			if err != nil || strings.TrimSpace(token) == "" {
				return "", errClaudeTokenUnavailable
			}
			return token, nil
		}
		token, err := r.secretStore.Reveal(ctx, secretID)
		if err != nil || strings.TrimSpace(token) == "" {
			return "", errClaudeTokenUnavailable
		}
		return token, nil
	}
}

// OpenCode model IDs are "<provider>/<model>"; the provider prefix selects the
// account the host OpenCode CLI authenticates with from its auth file.
const (
	openCodeGoPrefix   = "opencode-go/"
	openCodeZenPrefix  = "opencode/"
	openRouterPrefix   = "openrouter/"
	llmGatewayPrefix   = "llmgateway/"
	openCodeFreeSuffix = "-free"
)

func (r *usageBindingResolver) openCodeBinding(model string) (usageBinding, bool) {
	authPath := agentusage.OpenCodeAuthPath(r.home)
	switch {
	case strings.HasPrefix(model, openCodeGoPrefix):
		if strings.HasSuffix(model, openCodeFreeSuffix) {
			// Free Go models are not billed against the plan's spend windows and
			// the provider publishes no quota for them.
			return usageBinding{source: usageSourceNone, accountKey: "opencode-go-free:" + authPath, modelID: model}, true
		}
		return usageBinding{
			client:   agentusage.NewOpenCodeGoUsageClient(authPath),
			cacheKey: agentusage.CacheKey("opencode-go", authPath), source: usageSourceProviderAPI,
			accountKey: "opencode-go:" + authPath, modelID: model, class: fixedModelClass(agentusage.ModelClassStandard),
		}, true
	case strings.HasPrefix(model, openRouterPrefix):
		modelID := strings.TrimPrefix(model, openRouterPrefix)
		catalog, _ := r.catalogs()
		return usageBinding{
			client:   agentusage.NewOpenRouterUsageClient(authPath, r.openRouterDailyLimit),
			cacheKey: agentusage.CacheKey("openrouter", authPath), source: usageSourceProviderAPI,
			accountKey: "openrouter:" + authPath, modelID: model, class: catalogModelClass(catalog, modelID),
		}, true
	case strings.HasPrefix(model, llmGatewayPrefix):
		modelID := strings.TrimPrefix(model, llmGatewayPrefix)
		_, catalog := r.catalogs()
		return usageBinding{
			client:   agentusage.NewLLMGatewayUsageClient(authPath),
			cacheKey: agentusage.CacheKey("llmgateway", authPath), source: usageSourceProviderAPI,
			accountKey: "llmgateway:" + authPath, modelID: model, class: catalogModelClass(catalog, modelID),
		}, true
	case strings.HasPrefix(model, openCodeZenPrefix):
		// OpenCode Zen publishes no usage API; its free-model quota is counted
		// per account, so the binding only groups the account's profiles.
		return usageBinding{source: usageSourceNone, accountKey: "opencode-zen:" + authPath, modelID: model}, true
	default:
		return usageBinding{}, false
	}
}

// profileEnvLiteral returns a literal profile environment value. An entry that
// references a secret has no literal value and is read through the secret
// store instead.
func profileEnvLiteral(profile *settingsmodels.AgentProfile, key string) string {
	for _, env := range profile.EnvVars {
		if env.Key == key {
			return strings.TrimSpace(env.Value)
		}
	}
	return ""
}

// profileEnvSecretID returns the secret a profile environment entry references.
// A secret reference is the credential the agent itself runs with, so it
// outranks a literal value for the same key.
func profileEnvSecretID(profile *settingsmodels.AgentProfile, key string) string {
	for _, env := range profile.EnvVars {
		if env.Key == key {
			return strings.TrimSpace(env.SecretID)
		}
	}
	return ""
}
