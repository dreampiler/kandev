package state

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/db/dialect"
)

const (
	ChangeRequestStateOpen   = "open"
	ChangeRequestStateMerged = "merged"
	ChangeRequestStateClosed = "closed"
)

var ErrChangeRequestTargetChanged = errors.New("task change request target changed")

// ChangeRequestRecord is a Host-owned observation, not an operation receipt.
// Installation, workspace and provider identity are always supplied by the Host.
type ChangeRequestRecord struct {
	ID             string     `db:"id"`
	InstallationID string     `db:"installation_id"`
	WorkspaceID    string     `db:"workspace_id"`
	TaskID         string     `db:"task_id"`
	ProviderID     string     `db:"provider_id"`
	ProviderHost   string     `db:"provider_host"`
	RepositoryID   string     `db:"repository_id"`
	Number         int64      `db:"number"`
	URL            string     `db:"url"`
	Title          string     `db:"title"`
	State          string     `db:"state"`
	HeadBranch     string     `db:"head_branch"`
	BaseBranch     string     `db:"base_branch"`
	CreatedAt      time.Time  `db:"created_at"`
	MergedAt       *time.Time `db:"merged_at"`
	ClosedAt       *time.Time `db:"closed_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
}

// ChangeRequestStore shares the ordinary plugin-state schema owner and pool.
type ChangeRequestStore struct{ db, ro *sqlx.DB }

const changeRequestSchema = `
CREATE TABLE IF NOT EXISTS plugin_task_change_requests (
 id TEXT PRIMARY KEY, installation_id TEXT NOT NULL, workspace_id TEXT NOT NULL,
 task_id TEXT NOT NULL, provider_id TEXT NOT NULL, provider_host TEXT NOT NULL DEFAULT '',
 repository_id TEXT NOT NULL, number BIGINT NOT NULL, url TEXT NOT NULL DEFAULT '',
 title TEXT NOT NULL DEFAULT '', state TEXT NOT NULL DEFAULT 'open',
 head_branch TEXT NOT NULL DEFAULT '', base_branch TEXT NOT NULL DEFAULT '',
 created_at DATETIME NOT NULL, merged_at DATETIME, closed_at DATETIME, updated_at DATETIME NOT NULL,
 UNIQUE (installation_id, workspace_id, provider_id, provider_host, repository_id, number)
);
CREATE INDEX IF NOT EXISTS idx_plugin_task_change_requests_task ON plugin_task_change_requests(installation_id, workspace_id, task_id);
CREATE INDEX IF NOT EXISTS idx_plugin_task_change_requests_merged ON plugin_task_change_requests(workspace_id, state, merged_at);
`

// ChangeRequests exposes Host-owned records separately from plugin-writable KV.
func (s *Store) ChangeRequests() *ChangeRequestStore {
	if s == nil {
		return nil
	}
	return &ChangeRequestStore{db: s.db, ro: s.ro}
}

func (s *Store) initChangeRequestSchema() error {
	tx, err := s.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if !dialect.IsPostgres(s.db.DriverName()) {
		if err := migrateLegacyChangeRequests(tx); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(dialect.RenderSchema(s.db.DriverName(), changeRequestSchema)); err != nil {
		return err
	}
	return tx.Commit()
}

const changeRequestColumns = `id, installation_id, workspace_id, task_id, provider_id, provider_host,
 repository_id, number, url, title, state, head_branch, base_branch, created_at, merged_at, closed_at, updated_at`

// ReportChangeRequest atomically upserts a current task-owned observation.
// Stable identity and terminal merge facts survive repeated or stale polls.
func (s *ChangeRequestStore) ReportChangeRequest(ctx context.Context, row ChangeRequestRecord) (ChangeRequestRecord, error) {
	now := time.Now().UTC()
	createdProvided := !row.CreatedAt.IsZero()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	mergedAt := changeRequestKnownTimestamp(s.db.DriverName(), "merged_at")
	closedAt := changeRequestKnownTimestamp(s.db.DriverName(), "closed_at")
	reported, err := scanChangeRequest(s.db.QueryRowxContext(ctx, s.db.Rebind(`
 INSERT INTO plugin_task_change_requests (`+changeRequestColumns+`)
 SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?
 WHERE EXISTS (SELECT 1 FROM tasks WHERE id = ? AND workspace_id = ? AND archived_at IS NULL)
 ON CONFLICT (installation_id, workspace_id, provider_id, provider_host, repository_id, number)
 DO UPDATE SET task_id=excluded.task_id, url=excluded.url, title=excluded.title,
 state=CASE WHEN `+mergedAt+` IS NOT NULL THEN 'merged' ELSE excluded.state END,
 head_branch=excluded.head_branch, base_branch=excluded.base_branch,
 created_at=CASE WHEN ? THEN excluded.created_at ELSE plugin_task_change_requests.created_at END,
 merged_at=COALESCE(`+mergedAt+`, excluded.merged_at),
 closed_at=COALESCE(excluded.closed_at, `+closedAt+`),
 updated_at=excluded.updated_at
 RETURNING `+changeRequestColumns),
		uuid.NewString(), row.InstallationID, row.WorkspaceID, row.TaskID, row.ProviderID, row.ProviderHost,
		row.RepositoryID, row.Number, row.URL, row.Title, row.State, row.HeadBranch, row.BaseBranch,
		row.CreatedAt, row.MergedAt, row.ClosedAt, now, row.TaskID, row.WorkspaceID, createdProvided))
	if errors.Is(err, sql.ErrNoRows) {
		return ChangeRequestRecord{}, ErrChangeRequestTargetChanged
	}
	return reported, err
}

func changeRequestKnownTimestamp(driver, column string) string {
	qualified := "plugin_task_change_requests." + column
	if dialect.IsPostgres(driver) {
		return qualified
	}
	return "NULLIF(" + qualified + ",'')"
}

// RemoveChangeRequest is idempotent and cannot remove another installation's row.
func (s *ChangeRequestStore) RemoveChangeRequest(ctx context.Context, owner, workspace, provider, host, repository string, number int64) error {
	_, err := s.db.ExecContext(ctx, s.db.Rebind(`DELETE FROM plugin_task_change_requests
 WHERE installation_id=? AND workspace_id=? AND provider_id=? AND provider_host=? AND repository_id=? AND number=?`), owner, workspace, provider, host, repository, number)
	return err
}

// DeleteByInstallation removes projections when their reporting owner is disabled or uninstalled.
func (s *ChangeRequestStore) DeleteByInstallation(ctx context.Context, installationID string) error {
	_, err := s.db.ExecContext(ctx, s.db.Rebind(`DELETE FROM plugin_task_change_requests WHERE installation_id=?`), installationID)
	return err
}
