// Package state provides a plugin-scoped key/value store backed by SQLite.
// Plugins read and write arbitrary JSON values under a (scope, scope_id, key)
// tuple, always filtered by plugin_id so a plugin can never read or write
// another plugin's state (spec: docs/specs/plugins/requirements/plugins.md, "plugin_state").
package state

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/db"
)

// ChangeRequestState enumerates the lifecycle states a plugin-reported task
// change request can carry. Host consumers only treat "merged" as merged;
// every other value is shown but never counted as a merge.
const (
	ChangeRequestStateOpen   = "open"
	ChangeRequestStateMerged = "merged"
	ChangeRequestStateClosed = "closed"
)

var (
	ErrChangeRequestNotFound        = errors.New("plugin task change request was not found")
	ErrChangeRequestPayloadConflict = errors.New("plugin task change request idempotency payload conflict")
)

// ChangeRequestRecord is one Host-owned, plugin-reported task change request.
// The row is keyed by the owning installation and workspace plus the plugin's
// provider identity (provider id, repository id, number), so two plugins can
// never collide on the same forge object and a disabled or uninstalled plugin
// leaves rows that are fenced by owner rather than silently reused. The
// installation id is a Host-minted opaque principal (store.Record): the
// service maps it to the plugin id for uninstall fencing.
type ChangeRequestRecord struct {
	ID             string `db:"id"`
	InstallationID string `db:"installation_id"`
	WorkspaceID    string `db:"workspace_id"`
	TaskID         string `db:"task_id"`
	ProviderID     string `db:"provider_id"`
	ProviderHost   string `db:"provider_host"`
	RepositoryID   string `db:"repository_id"`
	Number         int64  `db:"number"`
	URL            string `db:"url"`
	Title          string `db:"title"`
	State          string `db:"state"`
	HeadBranch     string `db:"head_branch"`
	BaseBranch     string `db:"base_branch"`
	CreatedAt      string `db:"created_at"`
	MergedAt       string `db:"merged_at"`
	ClosedAt       string `db:"closed_at"`
	UpdatedAt      string `db:"updated_at"`
}

// ChangeRequestStore persists plugin-reported task change requests in the
// plugin_task_change_requests table. The owning installation stamps every
// row; uninstall and disable paths fence by that identity through
// DeleteByInstallation, so stale rows never outlive the plugin's authority.
type ChangeRequestStore struct {
	db *sqlx.DB
	ro *sqlx.DB
}

// NewChangeRequestStore creates a ChangeRequestStore and initializes the
// plugin_task_change_requests schema if needed.
func NewChangeRequestStore(pool *db.Pool) (*ChangeRequestStore, error) {
	if pool == nil || pool.Writer() == nil || pool.Reader() == nil {
		return nil, errors.New("plugin change request store: database pool is unavailable")
	}
	store := &ChangeRequestStore{db: pool.Writer(), ro: pool.Reader()}
	if err := store.initSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

// initChangeRequestSchemaDDL is the single source of truth for the table
// shape, shared by initSchema and conformance fixtures.
const initChangeRequestSchemaDDL = `
		CREATE TABLE IF NOT EXISTS plugin_task_change_requests (
			id TEXT PRIMARY KEY,
			installation_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			provider_host TEXT NOT NULL DEFAULT '',
			repository_id TEXT NOT NULL,
			number INTEGER NOT NULL,
			url TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'open',
			head_branch TEXT NOT NULL DEFAULT '',
			base_branch TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			merged_at TEXT NOT NULL DEFAULT '',
			closed_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			UNIQUE (installation_id, workspace_id, provider_id, repository_id, number)
		);
		CREATE INDEX IF NOT EXISTS idx_plugin_task_change_requests_task
			ON plugin_task_change_requests(installation_id, workspace_id, task_id);
		CREATE INDEX IF NOT EXISTS idx_plugin_task_change_requests_merged
			ON plugin_task_change_requests(workspace_id, state, merged_at);
	`

func (s *ChangeRequestStore) initSchema() error {
	_, err := s.db.Exec(initChangeRequestSchemaDDL)
	return err
}

// validChangeRequestState reports whether state is one of the three lifecycle
// values the contract admits. Anything else is rejected at the Host boundary
// before it can reach storage.
func validChangeRequestState(state string) bool {
	switch state {
	case ChangeRequestStateOpen, ChangeRequestStateMerged, ChangeRequestStateClosed:
		return true
	default:
		return false
	}
}

func validateChangeRequestRecord(record ChangeRequestRecord) error {
	if record.InstallationID == "" || record.WorkspaceID == "" || record.TaskID == "" ||
		record.ProviderID == "" || record.RepositoryID == "" || record.Number <= 0 {
		return errors.New("plugin task change request: required identity fields are missing")
	}
	if !validChangeRequestState(record.State) {
		return errors.New("plugin task change request: state is invalid")
	}
	return nil
}

func normalizeChangeRequestRecord(record ChangeRequestRecord, now string) ChangeRequestRecord {
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	if record.CreatedAt == "" {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	return record
}

// latchChangeRequestRecord preserves the fields a later report deliberately
// omits on an existing row: the original id and created timestamp, plus the
// merged and closed instants, so a stale report never un-merges a row.
func latchChangeRequestRecord(record, existing ChangeRequestRecord, now string) ChangeRequestRecord {
	record.ID = existing.ID
	if record.CreatedAt == "" || record.CreatedAt == now {
		record.CreatedAt = existing.CreatedAt
	}
	if record.MergedAt == "" {
		record.MergedAt = existing.MergedAt
	}
	if record.ClosedAt == "" {
		record.ClosedAt = existing.ClosedAt
	}
	return record
}

// ReportChangeRequest upserts one plugin-reported task change request. The
// owning installation, workspace, provider, repository, and number identify
// the row; every other field is replaced on conflict, so a plugin's latest
// report always wins over its own earlier reports. Merged and closed
// timestamps are latched: once set, a later report without one never clears
// it, so a stale poll cannot un-merge a row. The second return reports
// whether the row already existed.
func (s *ChangeRequestStore) ReportChangeRequest(ctx context.Context, record ChangeRequestRecord) (ChangeRequestRecord, bool, error) {
	if err := validateChangeRequestRecord(record); err != nil {
		return ChangeRequestRecord{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	record = normalizeChangeRequestRecord(record, now)
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return ChangeRequestRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var existing ChangeRequestRecord
	err = tx.GetContext(ctx, &existing, tx.Rebind(`SELECT `+changeRequestColumns+` FROM plugin_task_change_requests WHERE installation_id = ? AND workspace_id = ? AND provider_id = ? AND repository_id = ? AND number = ?`),
		record.InstallationID, record.WorkspaceID, record.ProviderID, record.RepositoryID, record.Number)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ChangeRequestRecord{}, false, err
	}
	created := err != nil
	if !created {
		record = latchChangeRequestRecord(record, existing, now)
	}
	_, err = tx.ExecContext(ctx, tx.Rebind(`
		INSERT INTO plugin_task_change_requests (
			id, installation_id, workspace_id, task_id, provider_id, provider_host,
			repository_id, number, url, title, state, head_branch, base_branch,
			created_at, merged_at, closed_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(installation_id, workspace_id, provider_id, repository_id, number)
		DO UPDATE SET task_id = excluded.task_id, url = excluded.url, title = excluded.title,
			state = excluded.state, head_branch = excluded.head_branch, base_branch = excluded.base_branch,
			created_at = excluded.created_at, merged_at = excluded.merged_at, closed_at = excluded.closed_at,
			updated_at = excluded.updated_at
	`), record.ID, record.InstallationID, record.WorkspaceID, record.TaskID, record.ProviderID, record.ProviderHost,
		record.RepositoryID, record.Number, record.URL, record.Title, record.State, record.HeadBranch, record.BaseBranch,
		record.CreatedAt, record.MergedAt, record.ClosedAt, record.UpdatedAt)
	if err != nil {
		return ChangeRequestRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ChangeRequestRecord{}, false, err
	}
	return record, !created, nil
}

// RemoveChangeRequest deletes one plugin-reported task change request owned
// by the calling installation. Removing a row that does not exist is not an
// error; removing another installation's row is impossible because every
// statement is scoped to the caller's installation id.
func (s *ChangeRequestStore) RemoveChangeRequest(ctx context.Context, installationID, workspaceID, providerID, repositoryID string, number int64) error {
	if installationID == "" || workspaceID == "" || providerID == "" || repositoryID == "" || number <= 0 {
		return errors.New("plugin task change request: required identity fields are missing")
	}
	_, err := s.db.ExecContext(ctx, s.db.Rebind(`
		DELETE FROM plugin_task_change_requests
		WHERE installation_id = ? AND workspace_id = ? AND provider_id = ? AND repository_id = ? AND number = ?
	`), installationID, workspaceID, providerID, repositoryID, number)
	return err
}

// ListChangeRequestsByTask returns every change request the installation
// reported for one task, newest first. Reads are scoped to the caller's
// installation and workspace, so one plugin never sees another's rows.
func (s *ChangeRequestStore) ListChangeRequestsByTask(ctx context.Context, installationID, workspaceID, taskID string) ([]ChangeRequestRecord, error) {
	if installationID == "" || workspaceID == "" || taskID == "" {
		return nil, errors.New("plugin task change request: installation, workspace, and task are required")
	}
	var records []ChangeRequestRecord
	if err := s.ro.SelectContext(ctx, &records, s.ro.Rebind(
		`SELECT `+changeRequestColumns+` FROM plugin_task_change_requests WHERE installation_id = ? AND workspace_id = ? AND task_id = ? ORDER BY updated_at DESC, id`),
		installationID, workspaceID, taskID); err != nil {
		return nil, err
	}
	return records, nil
}

// ListMergedChangeRequests returns the merged change requests reported in
// the given workspaces since `since`, newest first, up to limit. Workspace
// ids are caller-supplied and unscoped to any one installation: the overview
// consumes every plugin's merged rows together, which is what makes a
// Forgejo merge visible next to a GitHub one.
func (s *ChangeRequestStore) ListMergedChangeRequests(ctx context.Context, workspaceIDs []string, since time.Time, limit int) ([]ChangeRequestRecord, error) {
	if len(workspaceIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(workspaceIDs))
	args := make([]any, 0, len(workspaceIDs)+2)
	for i, id := range workspaceIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `SELECT ` + changeRequestColumns + ` FROM plugin_task_change_requests WHERE workspace_id IN (` +
		strings.Join(placeholders, ",") + `) AND state = 'merged' AND merged_at <> '' AND merged_at >= ? ORDER BY merged_at DESC`
	args = append(args, since.UTC().Format(time.RFC3339Nano))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	var records []ChangeRequestRecord
	if err := s.ro.SelectContext(ctx, &records, s.ro.Rebind(query), args...); err != nil {
		return nil, err
	}
	return records, nil
}

// DeleteAllForPlugin is intentionally absent: the installation id is opaque
// and not derivable from the plugin id, so a plugin-wide delete cannot be
// scoped from this store alone. The Service uninstall and disable paths own
// the fencing instead — they resolve the plugin's installation ids and call
// DeleteByInstallation for each before the registry record is gone (the same
// ordering deletePluginAgentConversations uses: nothing destructive to the
// package or record has happened yet, so a retry is safe).

// DeleteByInstallation removes every change request row for one installation
// id. The uninstall and disable paths call this once per installation id the
// plugin's records name (listed before the registry record is gone), so no
// row outlives the installation that reported it.
func (s *ChangeRequestStore) DeleteByInstallation(ctx context.Context, installationID string) error {
	if installationID == "" {
		return errors.New("plugin task change request: installation id is required")
	}
	_, err := s.db.ExecContext(ctx, s.db.Rebind(`
		DELETE FROM plugin_task_change_requests WHERE installation_id = ?
	`), installationID)
	return err
}

const changeRequestColumns = `id, installation_id, workspace_id, task_id, provider_id, provider_host,
	repository_id, number, url, title, state, head_branch, base_branch,
	created_at, merged_at, closed_at, updated_at`
