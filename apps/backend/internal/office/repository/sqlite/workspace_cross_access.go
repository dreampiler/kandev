package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kandev/kandev/internal/office/models"
)

func (r *Repository) createWorkspaceCrossAccessTable() error {
	_, err := r.db.Exec(`
	CREATE TABLE IF NOT EXISTS office_workspace_cross_access (
		workspace_id          TEXT PRIMARY KEY,
		enabled               INTEGER NOT NULL DEFAULT 0,
		allowed_workspace_ids TEXT NOT NULL DEFAULT '[]',
		coordinator_task_ids  TEXT NOT NULL DEFAULT '{}',
		updated_at            TIMESTAMP NOT NULL
	);
	`)
	return err
}

type crossAccessRow struct {
	WorkspaceID         string    `db:"workspace_id"`
	Enabled             int       `db:"enabled"`
	AllowedWorkspaceIDs string    `db:"allowed_workspace_ids"`
	CoordinatorTaskIDs  string    `db:"coordinator_task_ids"`
	UpdatedAt           time.Time `db:"updated_at"`
}

// GetCrossWorkspaceAccess returns the workspace's cross-workspace access
// setting. A workspace without a row gets the disabled default; a read
// failure is returned so callers fail closed instead of assuming a value.
func (r *Repository) GetCrossWorkspaceAccess(ctx context.Context, workspaceID string) (models.CrossWorkspaceAccess, error) {
	out := models.CrossWorkspaceAccess{
		WorkspaceID:         workspaceID,
		AllowedWorkspaceIDs: []string{},
		CoordinatorTaskIDs:  map[string]string{},
	}
	var row crossAccessRow
	err := r.ro.QueryRowxContext(ctx, r.ro.Rebind(`
		SELECT workspace_id, enabled, allowed_workspace_ids, coordinator_task_ids, updated_at
		FROM office_workspace_cross_access WHERE workspace_id = ?
	`), workspaceID).StructScan(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("read cross-workspace access: %w", err)
	}
	out.Enabled = row.Enabled != 0
	out.UpdatedAt = row.UpdatedAt
	if row.AllowedWorkspaceIDs != "" {
		if err := json.Unmarshal([]byte(row.AllowedWorkspaceIDs), &out.AllowedWorkspaceIDs); err != nil {
			return out, fmt.Errorf("decode allowed_workspace_ids: %w", err)
		}
	}
	if row.CoordinatorTaskIDs != "" {
		if err := json.Unmarshal([]byte(row.CoordinatorTaskIDs), &out.CoordinatorTaskIDs); err != nil {
			return out, fmt.Errorf("decode coordinator_task_ids: %w", err)
		}
	}
	if out.AllowedWorkspaceIDs == nil {
		out.AllowedWorkspaceIDs = []string{}
	}
	if out.CoordinatorTaskIDs == nil {
		out.CoordinatorTaskIDs = map[string]string{}
	}
	return out, nil
}

// SetCrossWorkspaceAccess replaces the workspace's cross-workspace access
// setting.
func (r *Repository) SetCrossWorkspaceAccess(ctx context.Context, access models.CrossWorkspaceAccess) error {
	allowed := access.AllowedWorkspaceIDs
	if allowed == nil {
		allowed = []string{}
	}
	coordinators := access.CoordinatorTaskIDs
	if coordinators == nil {
		coordinators = map[string]string{}
	}
	allowedJSON, err := json.Marshal(allowed)
	if err != nil {
		return fmt.Errorf("encode allowed_workspace_ids: %w", err)
	}
	coordinatorsJSON, err := json.Marshal(coordinators)
	if err != nil {
		return fmt.Errorf("encode coordinator_task_ids: %w", err)
	}
	enabled := 0
	if access.Enabled {
		enabled = 1
	}
	_, err = r.db.ExecContext(ctx, r.db.Rebind(`
		INSERT INTO office_workspace_cross_access
			(workspace_id, enabled, allowed_workspace_ids, coordinator_task_ids, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET
			enabled = excluded.enabled,
			allowed_workspace_ids = excluded.allowed_workspace_ids,
			coordinator_task_ids = excluded.coordinator_task_ids,
			updated_at = excluded.updated_at
	`), access.WorkspaceID, enabled, string(allowedJSON), string(coordinatorsJSON), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("write cross-workspace access: %w", err)
	}
	return nil
}

// ListEnabledCrossWorkspaceAccess returns the workspaces whose
// cross-workspace access setting is on.
func (r *Repository) ListEnabledCrossWorkspaceAccess(ctx context.Context) ([]models.CrossWorkspaceAccess, error) {
	var ids []string
	if err := r.ro.SelectContext(ctx, &ids, r.ro.Rebind(`
		SELECT workspace_id FROM office_workspace_cross_access WHERE enabled = 1 ORDER BY workspace_id
	`)); err != nil {
		return nil, fmt.Errorf("list cross-workspace access: %w", err)
	}
	out := make([]models.CrossWorkspaceAccess, 0, len(ids))
	for _, id := range ids {
		access, err := r.GetCrossWorkspaceAccess(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, access)
	}
	return out, nil
}
