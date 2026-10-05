package lifecycle

import (
	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

// ProviderWindowObserver receives the subscription rate-limit window a running
// agent reported about its own account.
//
// The observation is backend state rather than conversation content, so it never
// reaches the orchestrator, the WebSocket gateway or the frontend; the observer
// is the only consumer.
type ProviderWindowObserver interface {
	// ObserveProviderWindow records one window for the given agent profile. The
	// profile is the execution's own profile, and the observer resolves it to the
	// account that profile's agent authenticates with.
	ObserveProviderWindow(profileID string, window streams.RateLimitWindow)
}

// SetProviderWindowObserver installs the subscription-window observer.
//
// Optional: with none installed, a reported window is dropped and subscription
// usage keeps coming only from the provider's own usage API.
func (m *Manager) SetProviderWindowObserver(observer ProviderWindowObserver) {
	m.providerWindowObserver = observer
}

// handleRateLimitWindowEvent records a reported subscription window. It is
// consumed rather than published: an account's utilization is not a message, a
// tool result or a conversation event, and returning true keeps it out of the
// orchestrator and the client stream.
func (m *Manager) handleRateLimitWindowEvent(execution *AgentExecution, event agentctl.AgentEvent) {
	if m.providerWindowObserver == nil || event.RateLimitWindow == nil {
		return
	}
	m.providerWindowObserver.ObserveProviderWindow(execution.AgentProfileID, *event.RateLimitWindow)
}
