package service

import (
	"context"
	"fmt"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
)

type RuntimeFootprintProvider interface {
	SnapshotRuntimeFootprint(context.Context) agentruntime.RuntimeFootprintSnapshot
}

func (s *Service) SetRuntimeFootprintProvider(provider RuntimeFootprintProvider) {
	s.runtimeFootprintProvider = provider
}

type WorkspaceRuntimeFootprint struct {
	LiveRuntimes       int                            `json:"live_runtimes"`
	WorkspaceRuntimes  int                            `json:"workspace_runtimes"`
	Processes          int                            `json:"processes"`
	CommittedBytes     uint64                         `json:"committed_bytes"`
	ResidentBytes      uint64                         `json:"resident_bytes"`
	UnreadableRuntimes int                            `json:"unreadable_runtimes"`
	Complete           bool                           `json:"complete"`
	ObservedAt         time.Time                      `json:"observed_at"`
	Runtimes           []WorkspaceRuntimeFootprintRow `json:"runtimes"`
}

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

// WorkspaceRuntimeFootprint authorizes and resolves workspace ownership before
// requesting one bounded observation. It never starts or stops an execution.
func (s *Service) WorkspaceRuntimeFootprint(ctx context.Context, workspaceID string) (*WorkspaceRuntimeFootprint, error) {
	if err := s.authorizeWorkspaceID(ctx, workspaceID); err != nil {
		return nil, err
	}
	view := &WorkspaceRuntimeFootprint{Runtimes: []WorkspaceRuntimeFootprintRow{}}
	if s.runtimeFootprintProvider == nil {
		return view, nil
	}
	owned, err := s.workspaceTaskIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	snapshot := s.runtimeFootprintProvider.SnapshotRuntimeFootprint(ctx)
	view.ObservedAt = time.Now().UTC()
	if !snapshot.Complete {
		return view, nil
	}
	view.Complete = true
	view.LiveRuntimes = snapshot.LiveRuntimes
	for _, row := range snapshot.Runtimes {
		if _, belongs := owned[row.TaskID]; row.TaskID == "" || !belongs {
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
			SessionID: row.SessionID, TaskID: row.TaskID, Status: row.Status,
			Processes: row.Processes, CommittedBytes: row.CommittedBytes, ResidentBytes: row.ResidentBytes,
			UnreadableProcesses: row.UnreadableProcesses, LastActivityAt: row.LastActivityAt,
		})
	}
	return view, nil
}

const workspaceFootprintPageSize = 10000

func (s *Service) workspaceTaskIDs(ctx context.Context, workspaceID string) (map[string]struct{}, error) {
	tasks, total, err := s.tasks.ListTasksByWorkspace(ctx, workspaceID, "", "", "", 1, workspaceFootprintPageSize, "", true, false, false, false)
	if err != nil {
		return nil, err
	}
	if total > len(tasks) {
		return nil, fmt.Errorf("workspace footprint task inventory exceeds bounded read")
	}
	ids := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task != nil {
			ids[task.ID] = struct{}{}
		}
	}
	return ids, nil
}
