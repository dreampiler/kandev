package lifecycle

import (
	"context"

	"github.com/kandev/kandev/internal/agent/runtime/activity"
)

func (m *Manager) launchWithoutSession(ctx context.Context, req *LaunchRequest) (*AgentExecution, error) {
	activityLease, err := m.acquireActivity(ctx, activity.KindExecutionStarting)
	if err != nil {
		return nil, err
	}
	transferredActivity := false
	defer func() {
		if !transferredActivity {
			activityLease.Release()
		}
	}()
	activityLease.SetKind(activity.KindExecutionPreparing)
	execution, launchErr := m.launchInternal(ctx, req)
	if launchErr == nil && req.StartAgent {
		m.markAgentStartPending(execution)
		m.trackActivity(executionActivityKey(execution.ID), activityLease)
		transferredActivity = true
	}
	return execution, launchErr
}
