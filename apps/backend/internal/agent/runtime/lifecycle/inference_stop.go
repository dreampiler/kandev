package lifecycle

import "context"

func (m *Manager) stopOwnedInstance(ctx context.Context, backend ExecutorBackend, instance *ExecutorInstance, force bool) error {
	return m.stopWorkspaceRuntime(ctx, backend, instance, force)
}
