package dashboard

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kandev/kandev/internal/office/models"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// WorkspaceLister returns the workspaces visible to the calling identity.
// Implemented by the task service's identity-scoped ListWorkspaces, so the
// aggregate never lists a workspace the caller cannot reach.
type WorkspaceLister interface {
	ListWorkspaces(ctx context.Context) ([]*taskmodels.Workspace, error)
}

// ErrWorkspaceAggregateUnavailable is returned when the aggregate endpoint is
// reached without a wired workspace lister.
var ErrWorkspaceAggregateUnavailable = errors.New("multi-workspace aggregate is not configured")

// WorkspaceAggregateEntry is one workspace row in the multi-workspace
// aggregate overview (GET /workspaces/aggregate).
type WorkspaceAggregateEntry struct {
	WorkspaceID      string `json:"workspace_id"`
	Name             string `json:"name"`
	TaskCount        int    `json:"task_count"`
	OpenTasks        int    `json:"open_tasks"`
	InProgressTasks  int    `json:"in_progress_tasks"`
	BlockedTasks     int    `json:"blocked_tasks"`
	DoneTasks        int    `json:"done_tasks"`
	PendingApprovals int    `json:"pending_approvals"`
	AgentCount       int    `json:"agent_count"`
	RunningAgents    int    `json:"running_agents"`
	// IsOffice reports whether the workspace has an Office workflow, so the
	// client can link to the right home view.
	IsOffice bool                      `json:"is_office"`
	Metrics  *OverviewWorkspaceMetrics `json:"metrics,omitempty"`
	Parents  []OverviewParentTask      `json:"parents,omitempty"`
	// Running lists the tasks that have a session executing right now, most
	// severe first. It is absent when none is, so the client can say so in one
	// line instead of rendering an empty list.
	Running []OverviewRunningTask `json:"running,omitempty"`
	// RunningTruncated counts the running tasks the limit left out, so the card
	// can say how many more exist rather than showing a list that stops for no
	// stated reason.
	RunningTruncated int `json:"running_truncated,omitempty"`
}

// WorkspaceAggregateResponse is the read-only multi-workspace overview: one
// entry per visible workspace. The recent-activity feed is not part of it; the
// overview reports the last 24 hours through Last24h instead.
type WorkspaceAggregateResponse struct {
	Workspaces []WorkspaceAggregateEntry `json:"workspaces"`
	// Scope is the caller's overview scope ("office" or "reachable").
	Scope       string    `json:"scope"`
	GeneratedAt time.Time `json:"generated_at"`
	ComputeMs   int64     `json:"compute_ms"`
	// The sections below are present only when the overview reader is wired.
	System          *OverviewSystem          `json:"system,omitempty"`
	Models          []OverviewModel          `json:"models,omitempty"`
	BlockedAccounts []OverviewBlockedAccount `json:"blocked_accounts,omitempty"`
	// BlockedCircuits are the open dynamic-routing resource circuits. Their
	// presence says nothing about availability on its own; System carries
	// whether the circuits source answered.
	BlockedCircuits []OverviewBlockedCircuit `json:"blocked_circuits,omitempty"`
	Last24h         []OverviewEvent          `json:"last_24h,omitempty"`
	NeedsHuman      []OverviewHumanItem      `json:"needs_human,omitempty"`
}

// workspaceAgentCounts holds the per-workspace agent totals for the aggregate.
type workspaceAgentCounts struct {
	total   int
	running int
}

// GetWorkspacesAggregate returns the read-only overview from the
// identity-scoped workspace list, narrowed by the caller's overview scope.
// One snapshot per (caller, scope) is computed with sequential batched reads
// and served from memory for overviewCacheTTL; concurrent misses share one
// computation.
func (s *DashboardService) GetWorkspacesAggregate(ctx context.Context) (*WorkspaceAggregateResponse, error) {
	return s.getWorkspacesAggregate(ctx, defaultOverviewStatsWindowHours)
}

// getWorkspacesAggregate is the period-aware entry point: the handler passes
// the validated `window_hours`, while in-process callers keep the default.
func (s *DashboardService) getWorkspacesAggregate(
	ctx context.Context, windowHours int,
) (*WorkspaceAggregateResponse, error) {
	snap, err := s.loadOverviewSnapshot(ctx, windowHours)
	if err != nil {
		return nil, err
	}
	return snap.resp, nil
}

// buildAggregateBase builds the per-workspace counts for the selected
// workspaces. Counts default to zero for a workspace with no matching rows.
func (s *DashboardService) buildAggregateBase(
	ctx context.Context, ordered []*taskmodels.Workspace, ids []string,
) (*WorkspaceAggregateResponse, error) {
	breakdowns, err := s.repo.QueryWorkspaceTaskBreakdowns(ctx, ids)
	if err != nil {
		return nil, err
	}
	approvals, err := s.repo.CountPendingApprovalsByWorkspaces(ctx, ids)
	if err != nil {
		return nil, err
	}
	agentCounts, err := s.aggregateAgentCounts(ctx)
	if err != nil {
		return nil, err
	}

	entries := make([]WorkspaceAggregateEntry, 0, len(ordered))
	for _, w := range ordered {
		bd := breakdowns[w.ID]
		agents := agentCounts[w.ID]
		entries = append(entries, WorkspaceAggregateEntry{
			WorkspaceID:      w.ID,
			Name:             w.Name,
			TaskCount:        bd.Open + bd.InProgress + bd.Blocked + bd.Done,
			OpenTasks:        bd.Open,
			InProgressTasks:  bd.InProgress,
			BlockedTasks:     bd.Blocked,
			DoneTasks:        bd.Done,
			PendingApprovals: approvals[w.ID],
			AgentCount:       agents.total,
			RunningAgents:    agents.running,
			IsOffice:         w.OfficeWorkflowID != "",
		})
	}
	return &WorkspaceAggregateResponse{Workspaces: entries}, nil
}

// aggregateAgentCounts totals and running-agents per workspace from the shared
// agent reader (empty workspace id = across all workspaces). A nil agent
// reader or a read failure yields an empty map, so every count stays zero.
func (s *DashboardService) aggregateAgentCounts(ctx context.Context) (map[string]workspaceAgentCounts, error) {
	out := map[string]workspaceAgentCounts{}
	if s.agents == nil {
		return out, nil
	}
	agents, err := s.agents.ListAgentInstances(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if a == nil || a.WorkspaceID == "" {
			continue
		}
		c := out[a.WorkspaceID]
		c.total++
		if a.Status == models.AgentStatusWorking {
			c.running++
		}
		out[a.WorkspaceID] = c
	}
	return out, nil
}

// getWorkspacesAggregate serves GET /workspaces/aggregate. It is a read-only
// overview of the workspaces in the caller's scope; the workspace list
// is already identity-scoped by the lister, so this handler does no
// per-workspace ownership loop.
func (h *Handler) getWorkspacesAggregate(c *gin.Context) {
	windowHours, ok := statsWindowFromQuery(c)
	if !ok {
		return
	}
	resp, err := h.svc.getWorkspacesAggregate(c.Request.Context(), windowHours)
	if err != nil {
		if errors.Is(err, ErrWorkspaceAggregateUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// statsWindowFromQuery reads and validates the `window_hours` request value for
// the project-statistics block. It writes a 400 and returns false for a value
// outside the supported set, so a caller cannot believe it selected a period
// the screen ignores. The parameter applies only to the statistics block; every
// other overview section keeps its own fixed window.
func statsWindowFromQuery(c *gin.Context) (int, bool) {
	windowHours, ok := ParseOverviewStatsWindow(c.Query("window_hours"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "window_hours must be 24, 168, or 720"})
		return 0, false
	}
	return windowHours, true
}
