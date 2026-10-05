package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/db/dialect"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// Read-only overview queries for the multi-workspace aggregate
// (GET /api/v1/office/workspaces/aggregate and its list routes). Every query
// is bounded by a workspace-id set (or ids derived from it), reads through
// the read-only handle, and never selects task_session_messages content or
// metadata. Aggregated timestamps are scanned as text because SQLite drops
// the declared column type on MAX()/subquery results.

// OverviewTaskRow is one open (not completed, not cancelled) task with the
// step-entry time and the number of unfinished blocker tasks. The step's own
// automation travels with it, so a reader can tell a step that starts work by
// itself from one that waits for a person without a second query.
type OverviewTaskRow struct {
	ID                 string `db:"id"`
	WorkspaceID        string `db:"workspace_id"`
	Title              string `db:"title"`
	State              string `db:"state"`
	ParentID           string `db:"parent_id"`
	StepName           string `db:"step_name"`
	StepID             string `db:"step_id"`
	StepEventsRaw      string `db:"step_events"`
	StepPullFromStepID string `db:"pull_from_step_id"`
	CreatedAtRaw       string `db:"created_at"`
	UpdatedAtRaw       string `db:"updated_at"`
	StepEnteredRaw     string `db:"step_entered_at"`
	OpenBlockers       int    `db:"open_blockers"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
	StepEnteredAt      time.Time
	ChildCount         int
	OpenChildCount     int
}

// OverviewSessionRow is one task session of an open task. ErrorMessage is
// capped in SQL so a large provider dump never leaves the database. IsPrimary
// marks the task's own session, so a reader can tell it from an auxiliary
// session the task ran alongside.
type OverviewSessionRow struct {
	ID             string `db:"id"`
	TaskID         string `db:"task_id"`
	State          string `db:"state"`
	AgentProfileID string `db:"agent_profile_id"`
	ErrorMessage   string `db:"error_message"`
	StartedAtRaw   string `db:"started_at"`
	UpdatedAtRaw   string `db:"updated_at"`
	IsPrimary      bool   `db:"is_primary"`
	StartedAt      time.Time
	UpdatedAt      time.Time
}

// OverviewQueueRow groups queued messages by receiving session.
type OverviewQueueRow struct {
	SessionID    string `db:"session_id"`
	TaskID       string `db:"task_id"`
	WorkspaceID  string `db:"workspace_id"`
	TaskTitle    string `db:"title"`
	SessionState string `db:"session_state"`
	QueuedBy     string `db:"queued_by"`
	Count        int    `db:"cnt"`
	OldestRaw    string `db:"oldest"`
	Oldest       time.Time
}

// OverviewCompletedRow is a task completed inside the look-back window.
type OverviewCompletedRow struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	Title        string `db:"title"`
	UpdatedAtRaw string `db:"updated_at"`
	UpdatedAt    time.Time
}

// OverviewProfileSessionRow is a session started inside the look-back window,
// keyed by agent profile, for the model card.
type OverviewProfileSessionRow struct {
	ID             string `db:"id"`
	TaskID         string `db:"task_id"`
	WorkspaceID    string `db:"workspace_id"`
	AgentProfileID string `db:"agent_profile_id"`
	State          string `db:"state"`
	ErrorMessage   string `db:"error_message"`
	StartedAtRaw   string `db:"started_at"`
	StartedAt      time.Time
}

// OverviewProfileRow names an agent profile and its agent for the model card
// links (the settings route addresses an agent by name).
type OverviewProfileRow struct {
	ID          string `db:"id"`
	AgentID     string `db:"agent_id"`
	AgentName   string `db:"agent_name"`
	Name        string `db:"name"`
	DisplayName string `db:"agent_display_name"`
}

// OverviewBlockedProviderRow is a provider-health row that is not healthy.
type OverviewBlockedProviderRow struct {
	WorkspaceID string         `db:"workspace_id"`
	ProviderID  string         `db:"provider_id"`
	Scope       string         `db:"scope"`
	ScopeValue  string         `db:"scope_value"`
	State       string         `db:"state"`
	ErrorCode   string         `db:"error_code"`
	RetryAtRaw  sql.NullString `db:"retry_at"`
	RetryAt     time.Time
}

// OverviewApprovalRow is a pending Office approval.
type OverviewApprovalRow struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	Type         string `db:"type"`
	CreatedAtRaw string `db:"created_at"`
	CreatedAt    time.Time
}

// OverviewAutomationTaskRow is an automation-created task inside the window.
type OverviewAutomationTaskRow struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	AutomationID string `db:"automation_id"`
	Title        string `db:"title"`
	CreatedAtRaw string `db:"created_at"`
	CreatedAt    time.Time
}

// ListOverviewOpenTasks returns the open tasks of the supplied workspaces with
// the time they entered their current step (latest task_step_transitions row,
// via its (task_id, occurred_at) index), their unfinished-blocker count, and
// the automation of the step they sit on.
// Child counts are folded in from the same set, so no parent_id index is used.
func (r *Repository) ListOverviewOpenTasks(ctx context.Context, workspaceIDs []string) ([]*OverviewTaskRow, error) {
	var out []*OverviewTaskRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		var rows []*OverviewTaskRow
		query := `
			SELECT t.id, t.workspace_id, COALESCE(t.title, '') AS title, COALESCE(t.state, '') AS state,
			       COALESCE(t.parent_id, '') AS parent_id, COALESCE(ws.name, '') AS step_name,
			       COALESCE(ws.id, '') AS step_id,
			       COALESCE(ws.events, '') AS step_events,
			       COALESCE(ws.pull_from_step_id, '') AS pull_from_step_id,
			       CAST(t.created_at AS TEXT) AS created_at, CAST(t.updated_at AS TEXT) AS updated_at,
			       COALESCE(CAST((SELECT tr.occurred_at FROM task_step_transitions tr
			                      WHERE tr.task_id = t.id ORDER BY tr.occurred_at DESC LIMIT 1) AS TEXT), '') AS step_entered_at,
			       (SELECT COUNT(*) FROM task_blockers b JOIN tasks bt ON bt.id = b.blocker_task_id
			         WHERE b.task_id = t.id AND COALESCE(bt.state, '') NOT IN ('COMPLETED', 'CANCELLED')) AS open_blockers
			FROM tasks t
			LEFT JOIN workflow_steps ws ON ws.id = t.workflow_step_id
			WHERE t.workspace_id IN (` + strings.Join(placeholders, ",") + `)
			  AND t.is_ephemeral = 0` + andNotAutomationOrigin + `
			  AND t.archived_at IS NULL
			  AND COALESCE(t.state, '') NOT IN ('COMPLETED', 'CANCELLED')
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	byID := make(map[string]*OverviewTaskRow, len(out))
	for _, row := range out {
		row.CreatedAt = parseSqliteTime(row.CreatedAtRaw)
		row.UpdatedAt = parseSqliteTime(row.UpdatedAtRaw)
		row.StepEnteredAt = parseSqliteTime(row.StepEnteredRaw)
		if row.StepEnteredAt.IsZero() {
			row.StepEnteredAt = row.CreatedAt
		}
		byID[row.ID] = row
	}
	for _, row := range out {
		if parent := byID[row.ParentID]; parent != nil {
			parent.OpenChildCount++
		}
	}
	return out, nil
}

// CountOverviewChildren returns the total (any state, not archived) child
// count for each supplied parent task id.
func (r *Repository) CountOverviewChildren(ctx context.Context, parentIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(parentIDs))
	for _, batch := range workspaceIDBatches(parentIDs) {
		placeholders, args := placeholdersFor(batch)
		var rows []struct {
			ParentID string `db:"parent_id"`
			Count    int    `db:"cnt"`
		}
		query := `SELECT parent_id, COUNT(*) AS cnt FROM tasks
			WHERE parent_id IN (` + strings.Join(placeholders, ",") + `) AND archived_at IS NULL
			GROUP BY parent_id`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.ParentID] = row.Count
		}
	}
	return out, nil
}

// ListOverviewSessionsForTasks returns every session of the supplied tasks.
// Only the first 300 characters of error_message are read.
func (r *Repository) ListOverviewSessionsForTasks(ctx context.Context, taskIDs []string) ([]*OverviewSessionRow, error) {
	var out []*OverviewSessionRow
	for _, batch := range workspaceIDBatches(taskIDs) {
		placeholders, args := placeholdersFor(batch)
		var rows []*OverviewSessionRow
		query := `
			SELECT s.id, s.task_id, s.state, COALESCE(s.agent_profile_id, '') AS agent_profile_id,
			       SUBSTR(COALESCE(s.error_message, ''), 1, 300) AS error_message,
			       CAST(s.started_at AS TEXT) AS started_at, COALESCE(CAST(s.updated_at AS TEXT), '') AS updated_at,
			       COALESCE(s.is_primary, 0) AS is_primary
			FROM task_sessions s
			WHERE s.task_id IN (` + strings.Join(placeholders, ",") + `)
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	for _, row := range out {
		row.StartedAt = parseSqliteTime(row.StartedAtRaw)
		row.UpdatedAt = parseSqliteTime(row.UpdatedAtRaw)
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = row.StartedAt
		}
	}
	return out, nil
}

// LastAgentOutputBySession returns the newest agent-authored message time for
// each supplied session. Each lookup is one seek on the
// (task_session_id, author_type, created_at) index; message bodies are never
// read.
func (r *Repository) LastAgentOutputBySession(ctx context.Context, sessionIDs []string) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(sessionIDs))
	for _, batch := range workspaceIDBatches(sessionIDs) {
		placeholders, args := placeholdersFor(batch)
		var rows []struct {
			SessionID string `db:"session_id"`
			LastRaw   string `db:"last_at"`
		}
		query := `
			SELECT s.id AS session_id,
			       COALESCE(CAST((SELECT m.created_at FROM task_session_messages m
			                      WHERE m.task_session_id = s.id AND m.author_type = 'agent'
			                      ORDER BY m.created_at DESC LIMIT 1) AS TEXT), '') AS last_at
			FROM task_sessions s
			WHERE s.id IN (` + strings.Join(placeholders, ",") + `)
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if at := parseSqliteTime(row.LastRaw); !at.IsZero() {
				out[row.SessionID] = at
			}
		}
	}
	return out, nil
}

// ListOverviewQueues groups queued messages of the supplied workspaces by
// receiving session, with the receiving session state. Message content is
// not read here.
func (r *Repository) ListOverviewQueues(ctx context.Context, workspaceIDs []string) ([]*OverviewQueueRow, error) {
	var out []*OverviewQueueRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		var rows []*OverviewQueueRow
		query := `
			SELECT q.session_id, q.task_id, t.workspace_id, COALESCE(t.title, '') AS title,
			       COALESCE(s.state, '') AS session_state, MIN(q.queued_by) AS queued_by,
			       COUNT(*) AS cnt, CAST(MIN(q.queued_at) AS TEXT) AS oldest
			FROM queued_messages q
			JOIN tasks t ON t.id = q.task_id
			LEFT JOIN task_sessions s ON s.id = q.session_id
			WHERE t.workspace_id IN (` + strings.Join(placeholders, ",") + `)
			GROUP BY q.session_id, q.task_id, t.workspace_id, t.title, s.state
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	for _, row := range out {
		row.Oldest = parseSqliteTime(row.OldestRaw)
	}
	return out, nil
}

// FirstQueuedMessageBySession returns the first 400 characters of the
// head-of-queue message for each supplied session. Used only when the
// queued-messages list is expanded.
func (r *Repository) FirstQueuedMessageBySession(ctx context.Context, sessionIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(sessionIDs))
	for _, batch := range workspaceIDBatches(sessionIDs) {
		placeholders, args := placeholdersFor(batch)
		var rows []struct {
			SessionID string `db:"session_id"`
			Content   string `db:"content"`
		}
		query := `
			SELECT q.session_id, SUBSTR(q.content, 1, 400) AS content
			FROM queued_messages q
			WHERE q.session_id IN (` + strings.Join(placeholders, ",") + `)
			  AND q.position = (SELECT MIN(q2.position) FROM queued_messages q2 WHERE q2.session_id = q.session_id)
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		for _, row := range rows {
			out[row.SessionID] = row.Content
		}
	}
	return out, nil
}

// ListOverviewCompleted returns the total number of tasks completed since
// `since` per workspace plus the newest `limit` of them.
func (r *Repository) ListOverviewCompleted(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) (map[string]int, []*OverviewCompletedRow, error) {
	counts := make(map[string]int, len(workspaceIDs))
	var recent []*OverviewCompletedRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		args = append(args, since)
		var rows []*OverviewCompletedRow
		query := `
			SELECT t.id, t.workspace_id, COALESCE(t.title, '') AS title, CAST(t.updated_at AS TEXT) AS updated_at
			FROM tasks t
			WHERE t.workspace_id IN (` + strings.Join(placeholders, ",") + `)
			  AND t.is_ephemeral = 0` + andNotAutomationOrigin + `
			  AND t.state = 'COMPLETED' AND t.updated_at >= ?
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, nil, err
		}
		for _, row := range rows {
			row.UpdatedAt = parseSqliteTime(row.UpdatedAtRaw)
			counts[row.WorkspaceID]++
		}
		recent = append(recent, rows...)
	}
	sortByTimeDesc(recent, func(row *OverviewCompletedRow) time.Time { return row.UpdatedAt })
	if limit > 0 && len(recent) > limit {
		recent = recent[:limit]
	}
	return counts, recent, nil
}

// ListOverviewProfileSessions returns the sessions started since `since` in
// the supplied workspaces. The agent_profile_id IN (agent_profiles) driver
// lets the (agent_profile_id, started_at) index seek per profile instead of
// scanning every session.
func (r *Repository) ListOverviewProfileSessions(
	ctx context.Context, workspaceIDs []string, since time.Time,
) ([]*OverviewProfileSessionRow, error) {
	var out []*OverviewProfileSessionRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		args = append([]interface{}{since}, args...)
		var rows []*OverviewProfileSessionRow
		query := `
			SELECT s.id, s.task_id, t.workspace_id, COALESCE(s.agent_profile_id, '') AS agent_profile_id, s.state,
			       SUBSTR(COALESCE(s.error_message, ''), 1, 300) AS error_message,
			       CAST(s.started_at AS TEXT) AS started_at
			FROM task_sessions s
			JOIN tasks t ON t.id = s.task_id
			WHERE s.agent_profile_id IN (SELECT p.id FROM agent_profiles p)
			  AND s.started_at >= ?
			  AND t.workspace_id IN (` + strings.Join(placeholders, ",") + `)
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	for _, row := range out {
		row.StartedAt = parseSqliteTime(row.StartedAtRaw)
	}
	return out, nil
}

// ListOverviewProfiles returns the name and owning agent of each profile id.
func (r *Repository) ListOverviewProfiles(ctx context.Context, profileIDs []string) ([]*OverviewProfileRow, error) {
	var out []*OverviewProfileRow
	for _, batch := range workspaceIDBatches(profileIDs) {
		placeholders, args := placeholdersFor(batch)
		var rows []*OverviewProfileRow
		query := `SELECT p.id, p.agent_id, COALESCE(a.name, '') AS agent_name, COALESCE(p.name, '') AS name,
			       COALESCE(p.agent_display_name, '') AS agent_display_name
			FROM agent_profiles p LEFT JOIN agents a ON a.id = p.agent_id
			WHERE p.id IN (` + strings.Join(placeholders, ",") + `)`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// ListOverviewBlockedProviders returns the provider-health rows of the
// supplied workspaces that are not healthy.
func (r *Repository) ListOverviewBlockedProviders(
	ctx context.Context, workspaceIDs []string,
) ([]*OverviewBlockedProviderRow, error) {
	var out []*OverviewBlockedProviderRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		args = append(args, HealthStateHealthy)
		var rows []*OverviewBlockedProviderRow
		query := `
			SELECT workspace_id, provider_id, scope, scope_value, state, COALESCE(error_code, '') AS error_code,
			       CAST(retry_at AS TEXT) AS retry_at
			FROM office_provider_health
			WHERE workspace_id IN (` + strings.Join(placeholders, ",") + `) AND state != ?
		`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	for _, row := range out {
		if row.RetryAtRaw.Valid {
			row.RetryAt = parseSqliteTime(row.RetryAtRaw.String)
		}
	}
	return out, nil
}

// ListOverviewPendingApprovals returns pending Office approvals, oldest first.
func (r *Repository) ListOverviewPendingApprovals(
	ctx context.Context, workspaceIDs []string, limit int,
) ([]*OverviewApprovalRow, error) {
	var out []*OverviewApprovalRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		args = append(args, limit)
		var rows []*OverviewApprovalRow
		query := `SELECT id, workspace_id, type, CAST(created_at AS TEXT) AS created_at
			FROM office_approvals
			WHERE workspace_id IN (` + strings.Join(placeholders, ",") + `) AND status = 'pending'
			ORDER BY created_at ASC LIMIT ?`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	for _, row := range out {
		row.CreatedAt = parseSqliteTime(row.CreatedAtRaw)
	}
	return out, nil
}

// ListOverviewAutomationTasks returns automation-created tasks since `since`,
// newest first, up to limit.
func (r *Repository) ListOverviewAutomationTasks(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) ([]*OverviewAutomationTaskRow, error) {
	var out []*OverviewAutomationTaskRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		args = append(args, since, limit)
		var rows []*OverviewAutomationTaskRow
		query := `SELECT id, workspace_id, COALESCE(title, '') AS title, CAST(created_at AS TEXT) AS created_at,
			COALESCE(` + dialect.JSONExtract(r.ro.DriverName(), "metadata", "automation_id") + `, '') AS automation_id
			FROM tasks
			WHERE workspace_id IN (` + strings.Join(placeholders, ",") + `)
			  AND COALESCE(origin, '') = '` + taskmodels.TaskOriginAutomationRun + `' AND created_at >= ?
			ORDER BY created_at DESC LIMIT ?`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	for _, row := range out {
		row.CreatedAt = parseSqliteTime(row.CreatedAtRaw)
	}
	sortByTimeDesc(out, func(row *OverviewAutomationTaskRow) time.Time { return row.CreatedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// OverviewCreatedTaskRow is a task created inside the window that no other
// last-24-hours source already reports. ParentTitle names the task it was
// created under, empty for a top-level task.
type OverviewCreatedTaskRow struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	Title        string `db:"title"`
	ParentTitle  string `db:"parent_title"`
	CreatedAtRaw string `db:"created_at"`
	CreatedAt    time.Time
}

// ListOverviewCreatedTasks returns the tasks created since `since` in the
// supplied workspaces, newest first, up to limit. Automation-created tasks are
// left out because the automation source already reports them.
func (r *Repository) ListOverviewCreatedTasks(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) ([]*OverviewCreatedTaskRow, error) {
	var out []*OverviewCreatedTaskRow
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, args := placeholdersFor(batch)
		args = append(args, since, limit)
		var rows []*OverviewCreatedTaskRow
		query := `
			SELECT t.id, t.workspace_id, COALESCE(t.title, '') AS title,
			       COALESCE(p.title, '') AS parent_title,
			       CAST(t.created_at AS TEXT) AS created_at
			FROM tasks t
			LEFT JOIN tasks p ON p.id = t.parent_id
			WHERE t.workspace_id IN (` + strings.Join(placeholders, ",") + `)
			  AND t.is_ephemeral = 0` + andNotAutomationOriginT + `
			  AND t.created_at >= ?
			ORDER BY t.created_at DESC LIMIT ?`
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), args...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	for _, row := range out {
		row.CreatedAt = parseSqliteTime(row.CreatedAtRaw)
	}
	sortByTimeDesc(out, func(row *OverviewCreatedTaskRow) time.Time { return row.CreatedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sortByTimeDesc[T any](rows []T, at func(T) time.Time) {
	sort.SliceStable(rows, func(i, j int) bool { return at(rows[i]).After(at(rows[j])) })
}
