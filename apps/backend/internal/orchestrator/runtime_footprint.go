package orchestrator

import (
	"context"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agentruntime"
	"github.com/kandev/kandev/internal/common/runtimemetrics"
	"go.uber.org/zap"
)

// SessionRuntimeFootprint is one session's footprint row on the diagnostic
// surface. It carries counts, bytes, runtime state, and timestamps only, never
// transcript content, a credential, a resume token, or a command line.
type SessionRuntimeFootprint struct {
	SessionID           string    `json:"session_id"`
	TaskID              string    `json:"task_id,omitempty"`
	ExecutionID         string    `json:"execution_id,omitempty"`
	Runtime             string    `json:"runtime,omitempty"`
	Status              string    `json:"status,omitempty"`
	Processes           int       `json:"processes"`
	CommittedBytes      uint64    `json:"committed_bytes"`
	ResidentBytes       uint64    `json:"resident_bytes"`
	UnreadableProcesses int       `json:"unreadable_processes"`
	LastActivityAt      time.Time `json:"last_activity_at"`
}

// RuntimeFootprintProjection is one observation of what the live agent runtimes
// cost, as an operator reads it. The per-session rows are bounded by the number
// of live runtimes this installation actually has.
type RuntimeFootprintProjection struct {
	// LiveRuntimes is the number of live runtimes observed.
	LiveRuntimes int `json:"live_runtimes"`
	// Processes is the summed owned-descendant process count.
	Processes int `json:"processes"`
	// CommittedBytes is the summed commit charge, or zero on a platform that
	// does not report it per process.
	CommittedBytes uint64 `json:"committed_bytes"`
	// ResidentBytes is the summed resident set.
	ResidentBytes uint64 `json:"resident_bytes"`
	// UnreadableRuntimes counts live runtimes that could not be fully measured.
	// When nonzero, the byte totals are a lower bound.
	UnreadableRuntimes int `json:"unreadable_runtimes"`
	// Complete is false when the most recent read failed. A caller must not read
	// the totals of an incomplete projection as current.
	Complete bool `json:"complete"`
	// ObservedAt is when this observation was taken.
	ObservedAt time.Time `json:"observed_at"`
	// Runtimes is the per-session attribution of those totals.
	Runtimes []SessionRuntimeFootprint `json:"runtimes"`
}

// runtimeFootprintObserver is the narrow runtime capability the maintenance tick
// uses to observe live footprint. A runtime tier without it simply does not
// publish a footprint projection.
type runtimeFootprintObserver interface {
	SnapshotRuntimeFootprint(context.Context) agentruntime.RuntimeFootprintSnapshot
}

// observeRuntimeFootprintOnce records what the live agent runtimes currently
// cost and refreshes the bounded per-session projection an operator reads to
// attribute that cost.
//
// It is an observation, never a precondition: nothing here gates a stop, and a
// failed read replaces the projection with an incomplete one rather than leaving
// a previous reading readable as if it were current. The read runs on the
// maintenance tick rather than on a request path, so it costs at most one
// bounded process walk per interval.
func (s *Service) observeRuntimeFootprintOnce(ctx context.Context) {
	observer, ok := s.agentManager.(runtimeFootprintObserver)
	if !ok || observer == nil {
		return
	}
	snapshot := observer.SnapshotRuntimeFootprint(ctx)
	publishRuntimeFootprint(snapshot)
	s.setRuntimeFootprintProjection(snapshot)

	if !snapshot.Complete {
		s.logger.Warn("agent runtime footprint observation incomplete; projection carries no totals")
		return
	}
	s.logger.Info("agent runtime footprint observed",
		zap.Int("live_runtimes", snapshot.LiveRuntimes),
		zap.Int("owned_processes", snapshot.ProcessCount),
		zap.Int("unreadable_runtimes", snapshot.UnreadableRuntimes),
		zap.Int64("committed_bytes", int64(snapshot.CommittedBytes)),
		zap.Int64("resident_bytes", int64(snapshot.ResidentBytes)))
}

// setRuntimeFootprintProjection replaces the bounded per-session footprint
// projection with this observation.
func (s *Service) setRuntimeFootprintProjection(snapshot agentruntime.RuntimeFootprintSnapshot) {
	rows := make([]SessionRuntimeFootprint, 0, len(snapshot.Runtimes))
	for _, runtime := range snapshot.Runtimes {
		rows = append(rows, SessionRuntimeFootprint{
			SessionID:           runtime.SessionID,
			TaskID:              runtime.TaskID,
			ExecutionID:         runtime.ExecutionID,
			Runtime:             runtime.Runtime,
			Status:              runtime.Status,
			Processes:           runtime.Processes,
			CommittedBytes:      runtime.CommittedBytes,
			ResidentBytes:       runtime.ResidentBytes,
			UnreadableProcesses: runtime.UnreadableProcesses,
			LastActivityAt:      runtime.LastActivityAt,
		})
	}

	projection := RuntimeFootprintProjection{
		LiveRuntimes:       snapshot.LiveRuntimes,
		Processes:          snapshot.ProcessCount,
		CommittedBytes:     snapshot.CommittedBytes,
		ResidentBytes:      snapshot.ResidentBytes,
		UnreadableRuntimes: snapshot.UnreadableRuntimes,
		Complete:           snapshot.Complete,
		ObservedAt:         time.Now().UTC(),
		Runtimes:           rows,
	}

	s.runtimeFootprintMu.Lock()
	defer s.runtimeFootprintMu.Unlock()
	s.runtimeFootprintProjection = projection
}

// RuntimeFootprint returns the last observed per-session footprint projection.
// An incomplete projection means the most recent read failed and its totals are
// not current.
func (s *Service) RuntimeFootprint() RuntimeFootprintProjection {
	s.runtimeFootprintMu.Lock()
	defer s.runtimeFootprintMu.Unlock()

	projection := s.runtimeFootprintProjection
	projection.Runtimes = make([]SessionRuntimeFootprint, len(s.runtimeFootprintProjection.Runtimes))
	copy(projection.Runtimes, s.runtimeFootprintProjection.Runtimes)
	return projection
}

// runtimeFootprintMu guards the projection the maintenance tick replaces and the
// diagnostic surface reads.

// publishRuntimeFootprint records one observation in the process-wide gauges. A
// failed observation is counted and zeroes the gauges rather than leaving a
// previous total readable as if it were still current.
func publishRuntimeFootprint(snapshot agentruntime.RuntimeFootprintSnapshot) {
	runtimemetrics.PublishFootprint(runtimemetrics.Footprint{
		LiveRuntimes:       snapshot.LiveRuntimes,
		ProcessCount:       snapshot.ProcessCount,
		CommittedBytes:     snapshot.CommittedBytes,
		ResidentBytes:      snapshot.ResidentBytes,
		UnreadableRuntimes: snapshot.UnreadableRuntimes,
		Complete:           snapshot.Complete,
	})
}

// RuntimeFootprintRows returns one row per live runtime the last observation
// found. It implements the task service's footprint provider so the read can be
// served on the workspace-scoped HTTP surface.
func (s *Service) RuntimeFootprintRows() []agentruntime.SessionRuntimeFootprint {
	projection := s.RuntimeFootprint()
	rows := make([]agentruntime.SessionRuntimeFootprint, 0, len(projection.Runtimes))
	for _, row := range projection.Runtimes {
		rows = append(rows, agentruntime.SessionRuntimeFootprint{
			SessionID:           row.SessionID,
			TaskID:              row.TaskID,
			ExecutionID:         row.ExecutionID,
			Runtime:             row.Runtime,
			Status:              row.Status,
			Processes:           row.Processes,
			CommittedBytes:      row.CommittedBytes,
			ResidentBytes:       row.ResidentBytes,
			UnreadableProcesses: row.UnreadableProcesses,
			LastActivityAt:      row.LastActivityAt,
		})
	}
	return rows
}

// RuntimeFootprintComplete reports whether the last observation succeeded.
func (s *Service) RuntimeFootprintComplete() bool {
	return s.RuntimeFootprint().Complete
}

// RuntimeFootprintObservedAt is when the last observation was taken.
func (s *Service) RuntimeFootprintObservedAt() time.Time {
	return s.RuntimeFootprint().ObservedAt
}
