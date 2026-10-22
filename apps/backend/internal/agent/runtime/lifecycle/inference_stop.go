package lifecycle

import "context"

func (m *Manager) stopOwnedInstance(ctx context.Context, backend ExecutorBackend, instance *ExecutorInstance, force bool) error {
	return m.stopWorkspaceRuntime(ctx, backend, instance, force)
}

func (m *Manager) stopRuntimeInstanceAndRelease(ctx context.Context, backend ExecutorBackend, instance *ExecutorInstance, force bool) error {
	return m.stopWorkspaceRuntimeAndRelease(ctx, backend, instance, force)
}
