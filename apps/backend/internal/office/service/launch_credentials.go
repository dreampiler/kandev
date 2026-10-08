package service

import (
	"context"
	"fmt"
)

// RemintLaunchCredentials implements orchestrator.LaunchCredentialReminter,
// re-minting KANDEV_API_KEY / KANDEV_RUN_TOKEN for a launch that carries
// Office run identity (agent/workspace/run id) in env. sessionID is the task
// session the launch will use; when empty (a ceiling-deferred "start" replay
// that has no session yet) the run's persisted session id is used instead.
// The original env is returned unchanged when it carries no Office run
// identity (a non-Office launch, or an Office env this build's buildEnvVars
// shape has since dropped a field from) — there is nothing to re-mint, and
// the launch should proceed rather than fail outright.
func (si *SchedulerIntegration) RemintLaunchCredentials(
	ctx context.Context, taskID, sessionID string, env map[string]string,
) (map[string]string, error) {
	runID := env["KANDEV_RUN_ID"]
	agentID := env["KANDEV_AGENT_ID"]
	workspaceID := env["KANDEV_WORKSPACE_ID"]
	if runID == "" || agentID == "" || workspaceID == "" || si.svc.agentTokenMinter == nil {
		return env, nil
	}
	run, err := si.svc.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("launch credential re-mint: load run %s: %w", runID, err)
	}
	resolvedSessionID := sessionID
	if resolvedSessionID == "" {
		resolvedSessionID = run.SessionID
	}
	jwt, err := si.svc.agentTokenMinter.MintRuntimeJWT(
		agentID, taskID, workspaceID, runID, resolvedSessionID, run.Capabilities,
	)
	if err != nil {
		return nil, fmt.Errorf("launch credential re-mint: mint runtime jwt: %w", err)
	}
	refreshed := make(map[string]string, len(env))
	for k, v := range env {
		refreshed[k] = v
	}
	refreshed["KANDEV_API_KEY"] = jwt
	refreshed["KANDEV_RUN_TOKEN"] = jwt
	return refreshed, nil
}
