package hostutility

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/agent/agents"
)

// defaultInstallRecheckDelay is how long after boot an agent left not
// installed is measured again. It exceeds the discovery cache TTL, so the
// second measurement comes from a fresh sweep rather than the boot-time one.
const defaultInstallRecheckDelay = 60 * time.Second

// installRecheckTimeout bounds one background re-measurement: the shared
// detection sweep, instance creation, and one probe.
const installRecheckTimeout = probeTimeout + 30*time.Second

// InstallationSource answers whether an agent is installed from the shared
// discovery sweep, so capability status and the discovery endpoints agree.
// known is false when the source has no answer for the agent; the manager then
// measures the agent itself.
type InstallationSource interface {
	AgentAvailability(ctx context.Context, agentType string) (available, known bool, err error)
}

// SetInstallationSource wires the shared installation answer used before an
// agent is bootstrapped or its instance is recreated.
func (m *Manager) SetInstallationSource(source InstallationSource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installSource = source
}

// SetCapabilityChangeListener registers a callback run after a background
// re-measurement moves an agent out of not_installed. It must not block for
// long; it runs on the re-measurement goroutine.
func (m *Manager) SetCapabilityChangeListener(listener func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capabilityListener = listener
}

// agentInstalled answers from the installation source when it knows the agent
// and otherwise runs the agent's own detection.
func (m *Manager) agentInstalled(ctx context.Context, ag agents.Agent) (bool, error) {
	m.mu.RLock()
	source := m.installSource
	m.mu.RUnlock()
	if source != nil {
		available, known, err := source.AgentAvailability(ctx, ag.ID())
		if err == nil && known {
			return available, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
	}
	disc, err := ag.IsInstalled(ctx)
	if err != nil {
		return false, err
	}
	return disc != nil && disc.Available, nil
}

// RecheckNotInstalled re-measures, in the background, every listed agent whose
// capability record says not installed. Discovery calls it with the agents a
// sweep found available, so a record written by one failed measurement does
// not outlive the next successful one.
func (m *Manager) RecheckNotInstalled(agentTypes []string) {
	for _, agentType := range agentTypes {
		if !m.isNotInstalled(agentType) {
			continue
		}
		agentType := agentType
		m.goBackground(func(ctx context.Context) {
			m.recheckInstall(ctx, agentType)
		})
	}
}

// scheduleInstallRecheck measures once more, after installRecheckDelay, every
// agent the boot sweep left not installed.
func (m *Manager) scheduleInstallRecheck() {
	if len(m.notInstalledAgents()) == 0 {
		return
	}
	m.goBackground(func(ctx context.Context) {
		timer := time.NewTimer(m.installRecheckDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for _, agentType := range m.notInstalledAgents() {
			m.recheckInstall(ctx, agentType)
		}
	})
}

func (m *Manager) recheckInstall(ctx context.Context, agentType string) {
	_, _, _ = m.recheckGroup.Do(agentType, func() (interface{}, error) {
		if !m.isNotInstalled(agentType) {
			return nil, nil
		}
		ia, ok := m.registry.GetInferenceAgent(agentType)
		if !ok {
			return nil, nil
		}
		ag, ok := ia.(agents.Agent)
		if !ok {
			return nil, nil
		}
		recheckCtx, cancel := context.WithTimeout(ctx, installRecheckTimeout)
		defer cancel()
		if installed, err := m.agentInstalled(recheckCtx, ag); err != nil || !installed {
			return nil, nil
		}
		_, _ = m.Refresh(recheckCtx, agentType)
		if ctx.Err() != nil {
			return nil, nil
		}
		caps, _ := m.cache.get(agentType)
		m.log.Info("host utility install re-measured",
			zap.String("agent_type", agentType),
			zap.String("status", string(caps.Status)))
		if caps.Status != StatusNotInstalled {
			m.notifyCapabilityChange()
		}
		return nil, nil
	})
}

func (m *Manager) isNotInstalled(agentType string) bool {
	caps, ok := m.cache.get(agentType)
	return ok && caps.Status == StatusNotInstalled
}

func (m *Manager) notInstalledAgents() []string {
	var out []string
	for _, ia := range m.eligibleAgents() {
		ag := ia.(agents.Agent)
		if m.isNotInstalled(ag.ID()) {
			out = append(out, ag.ID())
		}
	}
	return out
}

func (m *Manager) notifyCapabilityChange() {
	m.mu.RLock()
	listener := m.capabilityListener
	m.mu.RUnlock()
	if listener != nil {
		listener()
	}
}

// goBackground runs fn on a goroutine Stop cancels and waits for. Nothing is
// started once the manager is stopped.
func (m *Manager) goBackground(fn func(context.Context)) {
	m.mu.Lock()
	if m.stopped || m.backgroundCtx == nil {
		m.mu.Unlock()
		return
	}
	ctx := m.backgroundCtx
	m.background.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.background.Done()
		fn(ctx)
	}()
}
