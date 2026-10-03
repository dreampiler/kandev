package service

import (
	"context"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agentruntime"
)

// RuntimeFootprintProvider surfaces the orchestrator's last observed live
// agent-runtime footprint. It is declared here rather than reusing the
// orchestrator's own projection type because this package must not depend on
// internal/orchestrator; the row shape lives in internal/agentruntime, which
// both tiers may import.
type RuntimeFootprintProvider interface {
	// RuntimeFootprintRows returns one row per live runtime the installation
	// observed. A row carries counts, bytes, runtime state, and timestamps only.
	RuntimeFootprintRows() []agentruntime.SessionRuntimeFootprint
	// RuntimeFootprintComplete reports whether the most recent observation
	// succeeded. A caller must not read the rows of an incomplete observation as
	// current.
	RuntimeFootprintComplete() bool
	// RuntimeFootprintObservedAt is when the observation was taken.
	RuntimeFootprintObservedAt() time.Time
}

// SetRuntimeFootprintProvider wires the live-runtime footprint source. Optional;
// when unset a workspace footprint read reports that no measurement is
// available rather than an empty reading that looks like zero memory in use.
func (s *Service) SetRuntimeFootprintProvider(provider RuntimeFootprintProvider) {
	s.runtimeFootprintProvider = provider
}

// WorkspaceRuntimeFootprint is one workspace's view of the installation's live
// agent runtimes. The rows are the subset whose task belongs to this workspace,
// so the installation-wide totals are not repeated per workspace and no other
// workspace's runtime is visible here.
type WorkspaceRuntimeFootprint struct {
	// LiveRuntimes is the number of live runtimes observed installation-wide.
	// It is context for the rows below, not a workspace total.
	LiveRuntimes int `json:"live_runtimes"`
	// WorkspaceRuntimes is the number of rows this workspace owns.
	WorkspaceRuntimes int `json:"workspace_runtimes"`
	// Processes is the summed owned-descendant process count over these rows.
	Processes int `json:"processes"`
	// CommittedBytes is the summed commit charge, or zero on a platform that does
	// not report it per process.
	CommittedBytes uint64 `json:"committed_bytes"`
	// ResidentBytes is the summed resident set over these rows.
	ResidentBytes uint64 `json:"resident_bytes"`
	// UnreadableRuntimes counts rows in this workspace the observation could not
	// fully measure. When nonzero, the byte totals are a lower bound.
	UnreadableRuntimes int `json:"unreadable_runtimes"`
	// Complete is false when the most recent observation failed or none exists.
	Complete bool `json:"complete"`
	// ObservedAt is when the observation was taken.
	ObservedAt time.Time `json:"observed_at"`
	// Runtimes is the per-session attribution for this workspace.
	Runtimes []WorkspaceRuntimeFootprintRow `json:"runtimes"`
}

// WorkspaceRuntimeFootprintRow is one session's footprint as this workspace sees
// it. It carries counts, bytes, runtime state, and timestamps only, never
// transcript content, a credential, a resume token, or a command line.
type WorkspaceRuntimeFootprintRow struct {
	SessionID           string    `json:"session_id"`
	TaskID              string    `json:"task_id"`
	Status              string    `json:"status,omitempty"`
	Processes           int       `json:"processes"`
	CommittedBytes      uint64    `json:"committed_bytes"`
	ResidentBytes       uint64    `json:"resident_bytes"`
	UnreadableProcesses int       `json:"unreadable_processes"`
	LastActivityAt      time.Time `json:"last_activity_at"`
}

// WorkspaceRuntimeFootprint returns the live agent-runtime footprint for the
// tasks in one workspace.
//
// It authorizes the workspace before reading anything, because the caller
// supplies the workspace identity. Rows whose task is not in this workspace are
// dropped rather than returned, so a workspace-scoped read cannot expose another
// workspace's runtime.
func (s *Service) WorkspaceRuntimeFootprint(ctx context.Context, workspaceID string) (*WorkspaceRuntimeFootprint, error) {
	if err := s.authorizeWorkspaceID(ctx, workspaceID); err != nil {
		return nil, err
	}
	if s.runtimeFootprintProvider == nil {
		return &WorkspaceRuntimeFootprint{}, nil
	}

	owned, err := s.workspaceTaskIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	rows := s.runtimeFootprintProvider.RuntimeFootprintRows()
	view := &WorkspaceRuntimeFootprint{
		LiveRuntimes: len(rows),
		Complete:     s.runtimeFootprintProvider.RuntimeFootprintComplete(),
		ObservedAt:   s.runtimeFootprintProvider.RuntimeFootprintObservedAt(),
		Runtimes:     make([]WorkspaceRuntimeFootprintRow, 0, len(rows)),
	}
	for _, row := range rows {
		// A row with no task identity cannot be attributed to any workspace, so
		// it stays in the installation totals and is not shown here.
		if _, inWorkspace := owned[row.TaskID]; row.TaskID == "" || !inWorkspace {
			continue
		}
		view.WorkspaceRuntimes++
		view.Processes += row.Processes
		view.CommittedBytes += row.CommittedBytes
		view.ResidentBytes += row.ResidentBytes
		if row.UnreadableProcesses > 0 {
			view.UnreadableRuntimes++
		}
		view.Runtimes = append(view.Runtimes, WorkspaceRuntimeFootprintRow{
			SessionID:           row.SessionID,
			TaskID:              row.TaskID,
			Status:              row.Status,
			Processes:           row.Processes,
			CommittedBytes:      row.CommittedBytes,
			ResidentBytes:       row.ResidentBytes,
			UnreadableProcesses: row.UnreadableProcesses,
			LastActivityAt:      row.LastActivityAt,
		})
	}
	return view, nil
}

// workspaceFootprintPageSize bounds the workspace task read. The page size is a
// guard against an unbounded read, not a display limit: a workspace with more
// tasks than this would understate its own footprint, so the size is set well
// above any realistic task count and the ceiling is checked rather than assumed.
const workspaceFootprintPageSize = 10000

// workspaceTaskIDs returns the set of task identities in one workspace.
func (s *Service) workspaceTaskIDs(ctx context.Context, workspaceID string) (map[string]struct{}, error) {
	tasks, _, err := s.tasks.ListTasksByWorkspace(
		ctx, workspaceID, "", "", "", 1, workspaceFootprintPageSize, "", true, false, false, false,
	)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task != nil {
			ids[task.ID] = struct{}{}
		}
	}
	return ids, nil
}
