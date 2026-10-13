package lifecycle

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/agent/agents"
	runtimeenv "github.com/kandev/kandev/internal/agent/runtime/environment"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/agentctl/server/utility"
	"github.com/kandev/kandev/internal/agentruntime"
	"github.com/kandev/kandev/pkg/agent"
)

// ResolveUtilityEnvironment uses the session launch's environment sources and
// secret resolver for a reviewer, independent of the writer's profile.
func (m *Manager) ResolveUtilityEnvironment(ctx context.Context, agentID string, values []settingsmodels.ProfileEnvVar) (map[string]string, error) {
	ag, ok := m.registry.Get(agentID)
	if !ok {
		return nil, fmt.Errorf("%w: agent %q is unavailable", utility.ErrLaunchUnavailable, agentID)
	}
	var definitions []runtimeenv.Definition
	appendProfileDefinitions(&definitions, values, runtimeenv.OriginAgentProfile)
	appendAgentRuntimeDefaults(&definitions, ag)
	appendRequiredCredentialDefinitions(ctx, &definitions, m.credsMgr, ag)
	env, _, err := runtimeenv.Resolve(ctx, definitions, m.resolveEnvironmentDefinition)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utility.ErrLaunchUnavailable, err)
	}
	return env, nil
}

// ResolveHostUtilityCommand applies the normal standalone command selection.
func (m *Manager) ResolveHostUtilityCommand(ctx context.Context, agentID string) (agents.Command, error) {
	return m.resolveUtilityCommand(ctx, agentID, agentruntime.RuntimeStandalone, nil)
}

func (m *Manager) resolveUtilityCommand(ctx context.Context, agentID string, runtime agentruntime.Runtime, metadata map[string]interface{}) (agents.Command, error) {
	ia, ok := m.registry.GetInferenceAgent(agentID)
	if !ok {
		return agents.Command{}, fmt.Errorf("%w: agent %q does not support inference", utility.ErrLaunchUnavailable, agentID)
	}
	cfg := ia.InferenceConfig()
	_, managed := ia.(agents.ManagedNPMRuntimeAgent)
	if !managed || (cfg.Protocol != "" && cfg.Protocol != agent.ProtocolACP) {
		return cfg.Command, nil
	}
	agentConfig, _ := ia.(agents.Agent)
	commands, err := m.buildAgentCommandWithContext(ctx, &LaunchRequest{}, nil, agentConfig, runtime,
		m.preferNativeBinary(agentConfig, runtime, metadata))
	if err != nil {
		return agents.Command{}, fmt.Errorf("%w: %w", utility.ErrLaunchUnavailable, err)
	}
	return agents.NewCommand(commands.args...), nil
}
