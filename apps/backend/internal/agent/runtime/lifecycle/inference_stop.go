package lifecycle

import (
	"context"
	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
)

func (m *Manager) stopOwnedInstance(ctx context.Context, backend ExecutorBackend, instance *ExecutorInstance, force bool) error {
	return backend.StopInstance(agentctl.WithInferenceStopObserver(ctx, m.inferenceConsumer), instance, force)
}

func (m *Manager) stopRuntimeInstanceAndRelease(ctx context.Context, backend ExecutorBackend, instance *ExecutorInstance, force bool) error {
	return stopRuntimeInstanceAndRelease(agentctl.WithInferenceStopObserver(ctx, m.inferenceConsumer), backend, instance, force)
}
