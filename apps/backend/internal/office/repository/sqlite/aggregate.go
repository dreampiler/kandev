package sqlite

import (
	"context"
	"strings"

	"github.com/kandev/kandev/internal/office/models"
)

// Cross-workspace aggregate queries for the read-only multi-workspace
// dashboard overview (GET /api/v1/office/workspaces/aggregate). Each method
// accepts a workspace-id set and answers in a single query, so the aggregate
// stays constant-cost regardless of how many workspaces the owner has.

// WorkspaceTaskCountRow is a raw row from the cross-workspace task-count query.
type WorkspaceTaskCountRow struct {
	WorkspaceID string `db:"workspace_id"`
	State       string `db:"state"`
	Count       int    `db:"count"`
}

// QueryWorkspaceTaskBreakdowns returns task counts grouped by state for each
// workspace in workspaceIDs, in a single query. Workspaces with no matching
// tasks are absent from the result map.
func (r *Repository) QueryWorkspaceTaskBreakdowns(ctx context.Context, workspaceIDs []string) (map[string]models.TaskBreakdown, error) {
	out := make(map[string]models.TaskBreakdown, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	placeholders, args := placeholdersFor(workspaceIDs)
	var rows []WorkspaceTaskCountRow
	err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(`
		SELECT workspace_id, COALESCE(state,'') as state, COUNT(*) as count
		FROM tasks
		WHERE workspace_id IN (`+strings.Join(placeholders, ",")+`)
		  AND is_ephemeral = 0`+andNotAutomationOrigin+`
		  AND archived_at IS NULL
		GROUP BY workspace_id, state
	`), args...)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		bd := out[row.WorkspaceID]
		bucketTaskBreakdownRow(&bd, row.State, row.Count)
		out[row.WorkspaceID] = bd
	}
	return out, nil
}

// CountPendingApprovalsByWorkspaces returns pending approval counts keyed by
// workspace id, for the supplied workspace set, in a single query.
func (r *Repository) CountPendingApprovalsByWorkspaces(ctx context.Context, workspaceIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	placeholders, args := placeholdersFor(workspaceIDs)
	var rows []struct {
		WorkspaceID string `db:"workspace_id"`
		Count       int    `db:"count"`
	}
	err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(`
		SELECT workspace_id, COUNT(*) as count
		FROM office_approvals
		WHERE workspace_id IN (`+strings.Join(placeholders, ",")+`)
		  AND status = 'pending'
		GROUP BY workspace_id
	`), args...)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.WorkspaceID] = row.Count
	}
	return out, nil
}

// ListActivityEntriesForWorkspaces returns the most recent activity entries
// across the supplied workspace set, newest first.
func (r *Repository) ListActivityEntriesForWorkspaces(ctx context.Context, workspaceIDs []string, limit int) ([]*models.ActivityEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	if len(workspaceIDs) == 0 {
		return []*models.ActivityEntry{}, nil
	}
	placeholders, args := placeholdersFor(workspaceIDs)
	args = append(args, limit)
	var entries []*models.ActivityEntry
	err := r.ro.SelectContext(ctx, &entries, r.ro.Rebind(
		`SELECT * FROM office_activity_log
		 WHERE workspace_id IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY created_at DESC LIMIT ?`),
		args...)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []*models.ActivityEntry{}
	}
	return entries, nil
}

// placeholdersFor builds a `?,?,...` list and the matching argument slice for
// a set of ids.
func placeholdersFor(ids []string) ([]string, []interface{}) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return placeholders, args
}

// bucketTaskBreakdownRow folds one state-count row into a TaskBreakdown.
// Shared with BucketTaskBreakdown so the per-workspace and cross-workspace
// breakdowns cannot drift on the state mapping.
func bucketTaskBreakdownRow(bd *models.TaskBreakdown, state string, count int) {
	switch state {
	case "COMPLETED":
		bd.Done += count
	case "IN_PROGRESS", "SCHEDULING":
		bd.InProgress += count
	case "BLOCKED":
		bd.Blocked += count
	default:
		bd.Open += count
	}
}
