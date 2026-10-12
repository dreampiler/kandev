package hostutility

import (
	"context"
	"fmt"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	utility "github.com/kandev/kandev/internal/agentctl/server/utility"
	"github.com/kandev/kandev/internal/common/acpprovider"
)

func (m *Manager) profilePromptEnvironment(ctx context.Context, profileID string, profile *settingsmodels.AgentProfile) (map[string]string, *acpprovider.GatewayAuth, error) {
	env, err := m.profileEnvironment(ctx, profile)
	if err != nil {
		return nil, nil, err
	}
	if profile.ProviderKind != settingsmodels.ProviderKindOpenAICompatible {
		return env, nil, nil
	}
	if m.providerGatewayAuthResolver == nil {
		return nil, nil, fmt.Errorf("%w: utility provider gateway resolver is not configured", utility.ErrLaunchUnavailable)
	}
	auth, keyName, key, err := m.providerGatewayAuthResolver(ctx, profileID, profile.AgentID)
	if err != nil {
		return nil, nil, err
	}
	if key != "" && keyName != "" {
		env[keyName] = key
	}
	return env, auth, nil
}

func (m *Manager) profileEnvironment(ctx context.Context, profile *settingsmodels.AgentProfile) (map[string]string, error) {
	if m.utilityEnvironmentResolver != nil {
		env, err := m.utilityEnvironmentResolver(ctx, profile.AgentID, profile.EnvVars)
		if err != nil {
			return nil, err
		}
		if env == nil {
			env = map[string]string{}
		}
		return env, nil
	}
	env := map[string]string{}
	for _, value := range profile.EnvVars {
		if value.SecretID != "" {
			return nil, fmt.Errorf("%w: profile environment resolver is not configured", utility.ErrLaunchUnavailable)
		}
		if value.Key != "" {
			env[value.Key] = value.Value
		}
	}
	return env, nil
}
