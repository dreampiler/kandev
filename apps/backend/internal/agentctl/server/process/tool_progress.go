package process

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/adapter"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

type toolProgressKey struct{ session, tool string }
type processIdentity struct {
	pid     int
	created uint64
}
type toolProcessObservation struct {
	command  string
	started  time.Time
	revision uint64
	root     processIdentity
	exited   bool
	cpu      map[processIdentity]uint64
}

func (m *Manager) recordToolProgress(event adapter.AgentEvent) {
	if event.SessionID == "" || event.ParentToolCallID != "" {
		return
	}
	m.toolProgressMu.Lock()
	defer m.toolProgressMu.Unlock()
	if event.Type == streams.EventTypeComplete || event.Type == streams.EventTypeError || event.Type == streams.EventTypeTurnStarted {
		for key := range m.toolProgress {
			if key.session == event.SessionID {
				delete(m.toolProgress, key)
			}
		}
		return
	}
	if event.ToolCallID == "" || (event.Type != "tool_call" && event.Type != "tool_update") {
		return
	}
	key := toolProgressKey{event.SessionID, event.ToolCallID}
	if terminalToolStatus(event.ToolStatus) {
		delete(m.toolProgress, key)
		return
	}
	if !executingToolStatus(event.ToolStatus) {
		return
	}
	m.observeStartedToolLocked(key, event)
}

func (m *Manager) observeStartedToolLocked(key toolProgressKey, event adapter.AgentEvent) {
	if m.toolProgress == nil {
		m.toolProgress = make(map[toolProgressKey]toolProcessObservation)
	}
	observation, exists := m.toolProgress[key]
	changed := !exists
	if !exists {
		observation.started = time.Now()
	}
	if payload := event.NormalizedPayload; payload != nil && payload.ShellExec() != nil {
		command := payload.ShellExec().Command
		if command != "" && command != observation.command {
			changed = true
			observation.command = command
			observation.root = processIdentity{}
			observation.cpu = nil
			observation.exited = false
		}
	}
	if changed {
		m.toolProgressRevision++
		observation.revision = m.toolProgressRevision
	}
	m.toolProgress[key] = observation
}

func (m *Manager) clearToolProgress() {
	m.toolProgressMu.Lock()
	defer m.toolProgressMu.Unlock()
	m.toolProgress = nil
	m.toolProgressRevision++
}

func executingToolStatus(status string) bool {
	return status == "in_progress" || status == "running" || status == "started"
}

func terminalToolStatus(status string) bool {
	switch status {
	case streams.EventTypeComplete, "completed", "success", streams.EventTypeError, "failed", "cancelled":
		return true
	default:
		return false
	}
}

// ProbeToolProgress samples in the execution's own host/container namespace.
func (m *Manager) ProbeToolProgress(ctx context.Context, req streams.ToolProgressRequest) streams.ToolProgressResponse {
	result := streams.ToolProgressResponse{Tools: make([]streams.ToolProgressObservation, 0, len(req.ToolCallIDs))}
	agentPID := m.toolAgentPID()
	table, err := readToolProcessTable(ctx, agentPID)
	for _, id := range req.ToolCallIDs {
		key := toolProgressKey{req.SessionID, id}
		m.toolProgressMu.Lock()
		observation, exists := m.toolProgress[key]
		m.toolProgressMu.Unlock()
		sample := streams.ToolProgressObservation{ToolCallID: id, ForegroundState: "unknown"}
		if err == nil && exists && observation.command != "" {
			updated, evidence := sampleToolProcesses(ctx, table, observation)
			sameProcess := m.toolAgentPID() == agentPID
			m.toolProgressMu.Lock()
			current, currentExists := m.toolProgress[key]
			if currentExists && current.revision == observation.revision && sameProcess {
				m.toolProgressRevision++
				updated.revision = m.toolProgressRevision
				m.toolProgress[key] = updated
				sample.ForegroundState = evidence.ForegroundState
				sample.CPUProgress = evidence.CPUProgress
			}
			m.toolProgressMu.Unlock()
		}
		result.Tools = append(result.Tools, sample)
	}
	return result
}

func (m *Manager) toolAgentPID() int {
	if !m.startMu.TryLock() {
		return 0
	}
	defer m.startMu.Unlock()
	return m.agentPID()
}
