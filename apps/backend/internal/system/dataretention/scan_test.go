package dataretention

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/system/settings"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
)

func testService(t *testing.T) *Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	w, err := db.OpenSQLite(path)
	require.NoError(t, err)
	r, err := db.OpenSQLiteReader(path)
	require.NoError(t, err)
	wx := sqlx.NewDb(w, "sqlite3")
	rx := sqlx.NewDb(r, "sqlite3")
	_, err = tasksqlite.NewWithDB(wx, rx, nil)
	require.NoError(t, err)
	pool := db.NewPool(wx, rx)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	_, err = settings.NewStore(pool)
	require.NoError(t, err)
	// queued_messages, pending_moves, and runs are owned by other schema owners,
	// so the eligibility contract's admission checks need them present.
	_, err = pool.Writer().Exec(`
		CREATE TABLE IF NOT EXISTS queued_messages(task_id TEXT, session_id TEXT);
		CREATE TABLE IF NOT EXISTS pending_moves(task_id TEXT, session_id TEXT);
		CREATE TABLE IF NOT EXISTS runs(status TEXT, payload TEXT, session_id TEXT, requested_at TIMESTAMP);
		CREATE INDEX IF NOT EXISTS idx_run_status_requested ON runs(status, requested_at);
	`)
	require.NoError(t, err)
	return New(pool)
}

// @covers AC-SYSTEM-PAGE-DB-HISTORY-RETENTION-005.2
func TestDefaultMessageWindowIsFourteenDays(t *testing.T) {
	policy := DefaultPolicy()
	require.Equal(t, 14, policy.ArchivedAge.Days)
	require.Equal(t, 7, policy.CleanupAge.Days)
}

// reducibleMetadata carries a shell_exec output large enough that removing the
// stdout/stderr payload nets a stored-byte reduction after the removal marker.
var reducibleMetadata = `{"normalized":{"kind":"shell_exec","shell_exec":{"output":{"stdout":"` +
	strings.Repeat("y", 8000) + `SENSITIVE","stderr":""}}}}`

func seedRetentionTask(t *testing.T, q *sqlx.DB, id, taskState, sessionState, at string, archived bool) {
	t.Helper()
	var archivedAt any
	if archived {
		archivedAt = at
	}
	var completedAt any
	if sessionState == "COMPLETED" {
		completedAt = at
	}
	_, err := q.Exec(`INSERT INTO tasks(id,title,state,archived_at,queued_for_step_id,created_at,updated_at)
		VALUES(?,?,?,?,'',?,?)`, id, "title", taskState, archivedAt, at, at)
	require.NoError(t, err)
	sessionID, turnID, messageID := "s-"+id, "t-"+id, "m-"+id
	_, err = q.Exec(`INSERT INTO task_sessions(id,task_id,state,started_at,completed_at,updated_at)
		VALUES(?,?,?,?,?,?)`, sessionID, id, sessionState, at, completedAt, at)
	require.NoError(t, err)
	_, err = q.Exec(`INSERT INTO task_session_turns(id,task_session_id,task_id,started_at,completed_at,created_at,updated_at,metadata)
		VALUES(?,?,?,?,?,?,?,'{}')`, turnID, sessionID, id, at, completedAt, at, at)
	require.NoError(t, err)
	_, err = q.Exec(`INSERT INTO task_session_messages(id,task_session_id,task_id,turn_id,author_type,content,type,metadata,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, messageID, sessionID, id, turnID, "agent", "content", "tool_execute", reducibleMetadata, at, at)
	require.NoError(t, err)
}

func messageMetadata(t *testing.T, q *sqlx.DB, id string) string {
	t.Helper()
	var metadata string
	require.NoError(t, q.Get(&metadata, `SELECT metadata FROM task_session_messages WHERE id=?`, id))
	return metadata
}

// enablePolicy saves an enabled skip-backup policy and runs its preparation, so
// the saved revision is approved and a cleanup pass is armed.
func enablePolicy(t *testing.T, s *Service) int64 {
	t.Helper()
	ctx := context.Background()
	status, err := s.Save(ctx, Update{Enabled: true, ArchivedAge: Age{Days: 14}, CleanupAge: Age{Days: 7}, BackupChoice: "skip"})
	require.NoError(t, err)
	require.NoError(t, s.tick(ctx))
	return status.Policy.Revision
}

// runPass drives the armed pass to a terminal state.
func runPass(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 200; i++ {
		require.NoError(t, s.tick(ctx))
		status, err := s.Get(ctx)
		require.NoError(t, err)
		if status.Operation == nil || status.Operation.State != stateRunning {
			return
		}
	}
	t.Fatal("pass did not settle")
}

// @covers AC-SYSTEM-PAGE-DB-HISTORY-RETENTION-001.1, 001.4, 002.6, 006.1
func TestCleanupPassReducesFinishedAndArchivedNotLive(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -30).Format(time.RFC3339Nano)
	recent := now.Format(time.RFC3339Nano)

	seedRetentionTask(t, s.pool.Writer(), "fin", "COMPLETED", "COMPLETED", old, false)
	seedRetentionTask(t, s.pool.Writer(), "arch", "COMPLETED", "COMPLETED", old, true)
	seedRetentionTask(t, s.pool.Writer(), "live", "RUNNING", "RUNNING", recent, false)

	enablePolicy(t, s)
	runPass(t, s)

	status, err := s.Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, status.LastRun)
	require.Equal(t, stateSucceeded, status.LastRun.State)
	require.GreaterOrEqual(t, status.LastRun.Messages.Reduced, int64(2))

	for _, id := range []string{"m-fin", "m-arch"} {
		metadata := messageMetadata(t, s.pool.Writer(), id)
		require.Contains(t, metadata, "payload_retention", id)
		require.NotContains(t, metadata, "SENSITIVE", id)
	}
	live := messageMetadata(t, s.pool.Writer(), "m-live")
	require.NotContains(t, live, "payload_retention")
	require.Contains(t, live, "SENSITIVE")
}

// @covers AC-SYSTEM-PAGE-DB-HISTORY-RETENTION-001.5
func TestCleanupPassIsIdempotentByMarker(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339Nano)
	seedRetentionTask(t, s.pool.Writer(), "fin", "COMPLETED", "COMPLETED", old, false)

	revision := enablePolicy(t, s)
	runPass(t, s)
	first := messageMetadata(t, s.pool.Writer(), "m-fin")
	require.Contains(t, first, "payload_retention")

	_, err := s.Run(ctx, revision)
	require.NoError(t, err)
	runPass(t, s)
	require.Equal(t, first, messageMetadata(t, s.pool.Writer(), "m-fin"))

	status, err := s.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), status.LastRun.Messages.Reduced)
	require.Equal(t, int64(0), status.LastRun.Messages.Eligible)
}
