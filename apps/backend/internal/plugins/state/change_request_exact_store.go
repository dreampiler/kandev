package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/db"
)

var (
	ErrChangeRequestTaskConflict = errors.New("plugin task change request is associated with a different task")
	ErrChangeRequestAmbiguous    = errors.New("plugin task change request identity has multiple provider hosts")
)

// ExactChangeRequestRecord is the exact-command projection of the same native
// ledger. The legacy row, timestamp reader and host-qualified identity remain
// owned by ChangeRequestStore; no second table or copied history is introduced.
type ExactChangeRequestRecord struct {
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

const changeRequestTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

// NewChangeRequestStore initializes the ledger through its existing schema owner.
func NewChangeRequestStore(pool *db.Pool) (*ChangeRequestStore, error) {
	if pool == nil || pool.Writer() == nil || pool.Reader() == nil {
		return nil, errors.New("plugin change request store: database pool is unavailable")
	}
	s := &ChangeRequestStore{db: pool.Writer(), ro: pool.Reader()}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ChangeRequestStore) initSchema() error {
	return (&Store{db: s.db, ro: s.ro}).initChangeRequestSchema()
}

func validateChangeRequestRecord(row ExactChangeRequestRecord) error {
	if row.InstallationID == "" || row.WorkspaceID == "" || row.TaskID == "" || row.ProviderID == "" || row.RepositoryID == "" || row.Number <= 0 {
		return errors.New("plugin task change request: required identity fields are missing")
	}
	if row.State != ChangeRequestStateOpen && row.State != ChangeRequestStateMerged && row.State != ChangeRequestStateClosed {
		return errors.New("plugin task change request: state is invalid")
	}
	if err := validateChangeRequestTimestamps(row); err != nil {
		return err
	}
	return validateChangeRequestStateTimes(row)
}

func validateChangeRequestTimestamps(row ExactChangeRequestRecord) error {
	for _, value := range []string{row.CreatedAt, row.MergedAt, row.ClosedAt} {
		if value != "" {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				return errors.New("plugin task change request: timestamp is invalid")
			}
		}
	}
	return nil
}

func validateChangeRequestStateTimes(row ExactChangeRequestRecord) error {
	switch row.State {
	case ChangeRequestStateOpen:
		if row.MergedAt != "" || row.ClosedAt != "" {
			return errors.New("plugin task change request: open state cannot carry terminal timestamps")
		}
	case ChangeRequestStateMerged:
		if row.MergedAt == "" || row.ClosedAt != "" {
			return errors.New("plugin task change request: merged state requires merged_at only")
		}
	case ChangeRequestStateClosed:
		if row.ClosedAt == "" || row.MergedAt != "" {
			return errors.New("plugin task change request: closed state requires closed_at only")
		}
	}
	return nil
}

func normalizeExactChangeRequest(row ExactChangeRequestRecord) ExactChangeRequestRecord {
	now := time.Now().UTC().Format(changeRequestTimestampLayout)
	if row.ID == "" {
		row.ID = uuid.NewString()
	}
	if row.CreatedAt == "" {
		row.CreatedAt = now
	}
	for _, field := range []*string{&row.CreatedAt, &row.MergedAt, &row.ClosedAt} {
		if *field != "" {
			parsed, _ := time.Parse(time.RFC3339Nano, *field)
			*field = parsed.UTC().Format(changeRequestTimestampLayout)
		}
	}
	row.UpdatedAt = now
	return row
}

func (s *ChangeRequestStore) ReportExactChangeRequest(ctx context.Context, row ExactChangeRequestRecord) (ExactChangeRequestRecord, bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return ExactChangeRequestRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	stored, created, err := s.reportExactChangeRequestTx(ctx, tx, row)
	if err != nil {
		return ExactChangeRequestRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ExactChangeRequestRecord{}, false, err
	}
	return stored, !created, nil
}

func (s *ChangeRequestStore) reportExactChangeRequestTx(ctx context.Context, tx *sqlx.Tx, row ExactChangeRequestRecord) (ExactChangeRequestRecord, bool, error) {
	if err := validateChangeRequestRecord(row); err != nil {
		return ExactChangeRequestRecord{}, false, err
	}
	row = normalizeExactChangeRequest(row)
	existing, err := lookupExactChangeRequest(ctx, tx, row)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ExactChangeRequestRecord{}, false, err
	}
	created := errors.Is(err, sql.ErrNoRows)
	if !created {
		if existing.TaskID != row.TaskID {
			return ExactChangeRequestRecord{}, false, ErrChangeRequestTaskConflict
		}
		// A host-less report may update the unique existing object but never
		// changes that object's native host-qualified identity.
		row.ProviderHost = existing.ProviderHost
	}
	merged := changeRequestKnownTimestamp(s.db.DriverName(), "merged_at")
	closed := changeRequestKnownTimestamp(s.db.DriverName(), "closed_at")
	_, err = tx.ExecContext(ctx, tx.Rebind(`
		INSERT INTO plugin_task_change_requests (`+changeRequestColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(installation_id, workspace_id, provider_id, provider_host, repository_id, number)
		DO UPDATE SET url = excluded.url, title = excluded.title,
			state = CASE WHEN plugin_task_change_requests.state IN ('merged', 'closed')
				THEN plugin_task_change_requests.state ELSE excluded.state END,
			head_branch = excluded.head_branch, base_branch = excluded.base_branch,
			merged_at = CASE WHEN plugin_task_change_requests.state = 'closed' THEN plugin_task_change_requests.merged_at
				ELSE COALESCE(`+merged+`, excluded.merged_at) END,
			closed_at = CASE WHEN plugin_task_change_requests.state = 'merged' THEN plugin_task_change_requests.closed_at
				ELSE COALESCE(`+closed+`, excluded.closed_at) END,
			updated_at = excluded.updated_at
		WHERE plugin_task_change_requests.task_id = excluded.task_id
	`), row.ID, row.InstallationID, row.WorkspaceID, row.TaskID, row.ProviderID, row.ProviderHost,
		row.RepositoryID, row.Number, row.URL, row.Title, row.State, row.HeadBranch, row.BaseBranch,
		row.CreatedAt, exactOptionalTimestamp(row.MergedAt), exactOptionalTimestamp(row.ClosedAt), row.UpdatedAt)
	if err != nil {
		return ExactChangeRequestRecord{}, false, err
	}
	stored, err := lookupExactChangeRequest(ctx, tx, row)
	if err != nil {
		return ExactChangeRequestRecord{}, false, err
	}
	if stored.TaskID != row.TaskID {
		return ExactChangeRequestRecord{}, false, ErrChangeRequestTaskConflict
	}
	return stored, created, nil
}

func lookupExactChangeRequest(ctx context.Context, tx *sqlx.Tx, row ExactChangeRequestRecord) (ExactChangeRequestRecord, error) {
	query := `SELECT ` + changeRequestColumns + ` FROM plugin_task_change_requests
		WHERE installation_id = ? AND workspace_id = ? AND provider_id = ? AND repository_id = ? AND number = ?`
	args := []any{row.InstallationID, row.WorkspaceID, row.ProviderID, row.RepositoryID, row.Number}
	if row.ProviderHost != "" {
		query += ` AND provider_host = ?`
		args = append(args, row.ProviderHost)
	}
	rows, err := tx.QueryxContext(ctx, tx.Rebind(query+` ORDER BY id LIMIT 2`), args...)
	if err != nil {
		return ExactChangeRequestRecord{}, err
	}
	defer func() { _ = rows.Close() }()
	var stored ExactChangeRequestRecord
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return stored, err
		}
		return stored, sql.ErrNoRows
	}
	native, err := scanChangeRequest(rows)
	if err != nil {
		return stored, err
	}
	stored = exactChangeRequestRecord(native)
	if rows.Next() {
		return ExactChangeRequestRecord{}, ErrChangeRequestAmbiguous
	}
	return stored, rows.Err()
}

func exactChangeRequestRecord(row ChangeRequestRecord) ExactChangeRequestRecord {
	format := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return value.UTC().Format(changeRequestTimestampLayout)
	}
	return ExactChangeRequestRecord{ID: row.ID, InstallationID: row.InstallationID, WorkspaceID: row.WorkspaceID,
		TaskID: row.TaskID, ProviderID: row.ProviderID, ProviderHost: row.ProviderHost, RepositoryID: row.RepositoryID,
		Number: row.Number, URL: row.URL, Title: row.Title, State: row.State, HeadBranch: row.HeadBranch, BaseBranch: row.BaseBranch,
		CreatedAt: format(&row.CreatedAt), MergedAt: format(row.MergedAt), ClosedAt: format(row.ClosedAt), UpdatedAt: format(&row.UpdatedAt)}
}

func exactOptionalTimestamp(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

// ChangeRequestResourceVersion is the public exact-command row-version token.
// Legacy RPC projections continue returning their original update timestamp.
func ChangeRequestResourceVersion(row ExactChangeRequestRecord) string {
	canonical := row.InstallationID + "\x00" + row.WorkspaceID + "\x00" + row.TaskID +
		"\x00" + row.ProviderID + "\x00" + row.ProviderHost + "\x00" + row.RepositoryID +
		"\x00" + row.ID + "\x00" + strconv.FormatInt(row.Number, 10) +
		"\x00" + row.State + "\x00" + row.UpdatedAt
	digest := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (s *ChangeRequestStore) ListChangeRequestsByTask(ctx context.Context, installationID, workspaceID, taskID string) ([]ExactChangeRequestRecord, error) {
	if installationID == "" || workspaceID == "" || taskID == "" {
		return nil, errors.New("plugin task change request: installation, workspace, and task are required")
	}
	rows, err := s.ro.QueryxContext(ctx, s.ro.Rebind(`SELECT `+changeRequestColumns+` FROM plugin_task_change_requests
		WHERE installation_id = ? AND workspace_id = ? AND task_id = ? ORDER BY updated_at DESC, id`), installationID, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var records []ExactChangeRequestRecord
	for rows.Next() {
		native, err := scanChangeRequest(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, exactChangeRequestRecord(native))
	}
	return records, rows.Err()
}

func (s *ChangeRequestStore) DeleteByWorkspace(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return errors.New("plugin task change request: workspace id is required")
	}
	_, err := s.db.ExecContext(ctx, s.db.Rebind(`DELETE FROM plugin_task_change_requests WHERE workspace_id = ?`), workspaceID)
	return err
}
