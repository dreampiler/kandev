package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

// historyScanFixture builds the minimal task/session/message schema the finished
// and archived eligibility checks read, with one finished, in-window task.
func historyScanFixture(t *testing.T) *sqlx.DB {
	t.Helper()
	d, err := sqlx.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, d.Close()) })
	d.SetMaxOpenConns(1)
	_, err = d.Exec(`
	CREATE TABLE tasks(id TEXT PRIMARY KEY,state TEXT, archived_at TIMESTAMP, queued_for_step_id TEXT,queued_at TIMESTAMP,created_at TIMESTAMP,updated_at TIMESTAMP);
	CREATE TABLE task_sessions(id TEXT PRIMARY KEY,task_id TEXT,state TEXT,started_at TIMESTAMP,completed_at TIMESTAMP,updated_at TIMESTAMP);
	CREATE INDEX sessions_task ON task_sessions(task_id);
	CREATE TABLE task_session_turns(id TEXT PRIMARY KEY,task_session_id TEXT,task_id TEXT,completed_at TIMESTAMP,updated_at TIMESTAMP,started_at TIMESTAMP,created_at TIMESTAMP,metadata TEXT DEFAULT '{}');
	CREATE INDEX turns_task ON task_session_turns(task_id);
	CREATE TABLE task_session_messages(id TEXT PRIMARY KEY,task_session_id TEXT,created_at TIMESTAMP,updated_at TIMESTAMP,type TEXT,metadata TEXT,requests_input INTEGER,task_id TEXT DEFAULT 'task',turn_id TEXT DEFAULT 'turn',author_type TEXT DEFAULT 'agent');
	CREATE INDEX idx_messages_session_updated ON task_session_messages(task_session_id,updated_at);
	CREATE INDEX idx_messages_session_created ON task_session_messages(task_session_id,created_at);
	CREATE INDEX idx_messages_task_author_created ON task_session_messages(task_id,author_type,type,created_at DESC);
	CREATE TABLE queued_messages(task_id TEXT,session_id TEXT);
	CREATE TABLE pending_moves(task_id TEXT,session_id TEXT);
	CREATE TABLE runs(status TEXT,payload TEXT,session_id TEXT,requested_at TIMESTAMP);
	CREATE INDEX idx_run_status_requested ON runs(status,requested_at);
	INSERT INTO tasks(id,state,queued_for_step_id,created_at,updated_at) VALUES('task','COMPLETED','','2020-01-01','2020-01-01');
	INSERT INTO task_sessions VALUES('session','task','COMPLETED','2020-01-01','2020-01-01','2020-01-01');
	INSERT INTO task_session_turns VALUES('turn','session','task','2020-01-01','2020-01-01','2020-01-01','2020-01-01','{}');
	INSERT INTO task_session_messages(id,task_session_id,created_at,updated_at,type,metadata,requests_input) VALUES('message','session','2020-01-01','2020-01-01','tool_execute','{}',0);
	`)
	require.NoError(t, err)
	return d
}

// @covers AC-SYSTEM-PAGE-DB-HISTORY-RETENTION-001.3
func TestHistoryTaskEligibilityCoversFinishedAndArchived(t *testing.T) {
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, mutation string
		want           bool
	}{
		{"finished terminal old", "", true},
		{"archived with nonterminal session", `UPDATE tasks SET archived_at='2020-01-01'; UPDATE task_sessions SET state='RUNNING'`, true},
		{"nonterminal in progress", `UPDATE tasks SET state='RUNNING'`, false},
		{"terminal but live session", `UPDATE task_sessions SET state='RUNNING'`, false},
		{"terminal but recent activity", `UPDATE task_session_messages SET updated_at='2026-06-01'`, false},
		{"archived in the future of the cutoff, nonterminal", `UPDATE tasks SET archived_at='2026-06-01', state='RUNNING'`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := historyScanFixture(t)
			if tt.mutation != "" {
				_, err := d.Exec(tt.mutation)
				require.NoError(t, err)
			}
			got, err := HistoryTaskEligible(context.Background(), d, "sqlite3", "task", cutoff)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// @covers AC-SYSTEM-PAGE-DB-HISTORY-RETENTION-001.1
func TestHistoryTaskEligibilityRejectsNonterminalQuiescentTask(t *testing.T) {
	d := historyScanFixture(t)
	_, err := d.Exec(`UPDATE tasks SET state='CREATED', updated_at='2020-01-01'`)
	require.NoError(t, err)
	got, err := FinishedTaskEligible(context.Background(), d, "task", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.False(t, got)
}

// @covers AC-SYSTEM-PAGE-DB-HISTORY-RETENTION-001.1
func TestHistoryCursorWalksFinishedAndArchivedOnly(t *testing.T) {
	d := historyScanFixture(t)
	_, err := d.Exec(`
	 INSERT INTO tasks(id,state,archived_at,queued_for_step_id,created_at,updated_at) VALUES
	  ('a-archived','REVIEW','2020-01-01','','2020-01-01','2020-01-01'),
	  ('b-running','RUNNING',NULL,'','2020-01-01','2020-01-01'),
	  ('c-created','CREATED',NULL,'','2020-01-01','2020-01-01')`)
	require.NoError(t, err)

	upper, err := HistoryUpperTaskID(context.Background(), d)
	require.NoError(t, err)
	require.Equal(t, "task", upper)

	var ids []string
	after := ""
	for {
		id, err := NextHistoryTaskID(context.Background(), d, after, upper)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		require.NoError(t, err)
		ids = append(ids, id)
		after = id
	}
	require.Equal(t, []string{"a-archived", "task"}, ids)
}
