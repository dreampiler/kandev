package lifecycle

import (
	"context"
	"time"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	agentruntime "github.com/kandev/kandev/internal/agentruntime"
	"github.com/kandev/kandev/internal/common/runtimemetrics"
	"go.uber.org/zap"
)

// footprintSnapshotTimeout bounds one footprint observation so a slow process
// walk on the control server cannot hold the maintenance tick open.
const footprintSnapshotTimeout = 15 * time.Second

// SessionRuntimeFootprint is one session's authoritative footprint row. It
// carries counts, bytes, runtime state, and timestamps only, never transcript
// content, a credential, a resume token, or a command line.
type SessionRuntimeFootprint struct {
	SessionID           string
	TaskID              string
	ExecutionID         string
	Runtime             string
	Status              string
	Processes           int
	CommittedBytes      uint64
	ResidentBytes       uint64
	UnreadableProcesses int
	LastActivityAt      time.Time
}

// RuntimeFootprintReader is the narrow per-runtime footprint capability the
// lifecycle facade exposes. A runtime whose processes live on another host does
// not implement it, because it has no host-local footprint to attribute.
type RuntimeFootprintReader interface {
	ReadRuntimeFootprints(ctx context.Context) ([]agentctl.RuntimeFootprint, error)
}

// SnapshotRuntimeFootprint reads the host-local control server's per-instance
// footprint and attributes it to the sessions and executions this backend owns.
//
// A reading with no attributable execution still counts toward the totals: the
// process is real and owned by this installation even when its session identity
// cannot be resolved, and dropping it would understate the footprint.
func (m *Manager) SnapshotRuntimeFootprint(ctx context.Context) agentruntime.RuntimeFootprintSnapshot {
	var snapshot agentruntime.RuntimeFootprintSnapshot
	if m == nil || m.executorRegistry == nil {
		return snapshot
	}
	reader, ok := m.executorRegistry.RuntimeFootprintReader()
	if !ok {
		return snapshot
	}

	readCtx, cancel := context.WithTimeout(ctx, footprintSnapshotTimeout)
	defer cancel()

	measured, err := reader.ReadRuntimeFootprints(readCtx)
	if err != nil {
		m.logger.Warn("runtime footprint observation failed; totals left unmeasured", zap.Error(err))
		return snapshot
	}
	snapshot.Complete = true
	for _, footprint := range measured {
		row, attributed := m.resolveFootprintRow(footprint)
		addFootprintReading(&snapshot, footprint, row, attributed)
	}
	return snapshot
}

// resolveFootprintRow names the execution that owns one reading. A reading whose
// session identity is not tracked here has no row and is left unattributed
// rather than mapped onto a guessed session.
func (m *Manager) resolveFootprintRow(footprint agentctl.RuntimeFootprint) (agentruntime.SessionRuntimeFootprint, bool) {
	if footprint.SessionID == "" || m.executionStore == nil {
		return agentruntime.SessionRuntimeFootprint{}, false
	}
	execution, exists := m.executionStore.GetBySessionID(footprint.SessionID)
	if !exists || execution == nil {
		return agentruntime.SessionRuntimeFootprint{}, false
	}
	return agentruntime.SessionRuntimeFootprint{
		SessionID:   execution.SessionID,
		TaskID:      execution.TaskID,
		ExecutionID: execution.ID,
		Runtime:     string(execution.RuntimeName),
	}, true
}

// add folds one control-server reading into the snapshot.
func addFootprintReading(s *agentruntime.RuntimeFootprintSnapshot, footprint agentctl.RuntimeFootprint, row agentruntime.SessionRuntimeFootprint, attributed bool) {
	s.LiveRuntimes++
	s.ProcessCount += footprint.Processes
	s.CommittedBytes += footprint.CommittedBytes
	s.ResidentBytes += footprint.ResidentBytes
	if footprint.UnreadableCount > 0 {
		s.UnreadableRuntimes++
	}
	if !attributed {
		return
	}
	row.Status = footprint.Status
	row.Processes = footprint.Processes
	row.CommittedBytes = footprint.CommittedBytes
	row.ResidentBytes = footprint.ResidentBytes
	row.UnreadableProcesses = footprint.UnreadableCount
	if footprint.LastActivity.After(row.LastActivityAt) {
		row.LastActivityAt = footprint.LastActivity
	}
	s.Runtimes = append(s.Runtimes, row)
}

// Publish records one observation in the process-wide gauges.
func PublishRuntimeFootprint(s agentruntime.RuntimeFootprintSnapshot) {
	runtimemetrics.PublishFootprint(runtimemetrics.Footprint{
		LiveRuntimes:       s.LiveRuntimes,
		ProcessCount:       s.ProcessCount,
		CommittedBytes:     s.CommittedBytes,
		ResidentBytes:      s.ResidentBytes,
		UnreadableRuntimes: s.UnreadableRuntimes,
		Complete:           s.Complete,
	})
}
