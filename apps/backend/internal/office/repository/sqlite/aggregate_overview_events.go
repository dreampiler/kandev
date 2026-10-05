package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// The reads behind the last-24-hours event kinds that no earlier overview query
// answered: model blocks and their clearing, merged change requests, failed
// automation runs, owner decisions, workflow-step moves, and what followed a
// failed session.
//
// None of these invent history. A resource-circuit row records the state it is
// in and when it last changed, so a block is reported from that row and a
// circuit that has already closed is reported as cleared at its own clear time
// rather than at some remembered moment this screen would have to invent.

// The two tables a model block can come from. Source says which one, because
// only a circuit's subject can be resolved into a profile and a model: a
// provider limit is already named by the provider it carries.
const (
	modelBlockSourceCircuit  = "circuit"
	modelBlockSourceProvider = "provider"
)

// OverviewModelBlockRow is one model account or provider limit that was blocked
// or that lifted inside the window. Resource names the subject as the circuit
// key or the provider id: the fingerprint inside a key is not something a
// reader can interpret, so the key is carried whole for the circuit subject
// and the provider is carried by name for a limit.
type OverviewModelBlockRow struct {
	Resource string `db:"resource"`
	// Source is the table the row came from, so a caller can tell a circuit key
	// from a provider id instead of inferring it from the subject's shape.
	Source string `db:"source"`
	// BlockedAt is when the block was last written; ClearedAt is when the
	// block is expected to lift. ClearedAt is absent for a resource that never
	// named a clear time, and none is supplied here.
	BlockedAtRaw string `db:"blocked_at"`
	ClearedAtRaw string `db:"cleared_at"`
	BlockedAt    time.Time
	ClearedAt    time.Time
	// Cleared reports that the block had already lifted by `now`, so the
	// client can show it as lifted rather than as still blocking.
	Cleared bool   `db:"cleared"`
	Code    string `db:"code"`
}

// IsCircuit reports that the row describes a resource circuit rather than a
// provider limit. Only a circuit carries a key that can be resolved into a
// profile and a model, so this is what decides whether the subject is nameable.
func (r *OverviewModelBlockRow) IsCircuit() bool {
	return r.Source == modelBlockSourceCircuit
}

// ListOverviewModelBlocks returns the model blocks written since `since` or
// that lifted since `since`, newest first, up to limit. Only circuits and
// provider limits that actually carry a block are read: a healthy resource is
// not an event.
func (r *Repository) ListOverviewModelBlocks(
	ctx context.Context, since, now time.Time, limit int,
) ([]*OverviewModelBlockRow, error) {
	var rows []*OverviewModelBlockRow
	query := `
		SELECT resource, source, CAST(blocked_at AS TEXT) AS blocked_at,
		       CAST(cleared_at AS TEXT) AS cleared_at, cleared, code
		FROM (
			SELECT resource_key AS resource, '` + modelBlockSourceCircuit + `' AS source,
			       updated_at AS blocked_at, until_at AS cleared_at,
			       CASE WHEN until_at IS NOT NULL AND until_at <= ? THEN 1 ELSE 0 END AS cleared,
			       code
			FROM dynamic_resource_circuits
			WHERE state <> 'closed' OR strikes > 0
			UNION ALL
			SELECT provider AS resource, '` + modelBlockSourceProvider + `' AS source,
			       updated_at AS blocked_at, block_until AS cleared_at,
			       CASE WHEN block_until <= ? THEN 1 ELSE 0 END AS cleared,
			       '' AS code
			FROM dynamic_provider_limits
			WHERE block_until IS NOT NULL
		)
		WHERE blocked_at >= ? OR (cleared_at IS NOT NULL AND cleared_at >= ?)
		ORDER BY blocked_at DESC LIMIT ?`
	if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), now, now, since, since, limit); err != nil {
		return nil, err
	}
	for _, row := range rows {
		row.BlockedAt = parseSqliteTime(row.BlockedAtRaw)
		row.ClearedAt = parseSqliteTime(row.ClearedAtRaw)
	}
	return rows, nil
}

// readOverviewBatched runs one workspace-scoped query once per batch of
// workspace ids and merges the results, which is how every workspace-scoped
// overview read already behaves: a caller may name more workspaces than a single
// statement can bind. The statement names the batch position as a single %s, so
// the batching and the merge live here instead of being restated per query, and
// the trailing args are the statement's own bound values. The row type is named
// by the caller because a generic result position alone does not fix it.
func readOverviewBatched[T any](
	ctx context.Context, r *Repository, workspaceIDs []string, stmtFormat string, args ...interface{},
) ([]*T, error) {
	var out []*T
	for _, batch := range workspaceIDBatches(workspaceIDs) {
		placeholders, batchArgs := placeholdersFor(batch)
		var rows []*T
		stmt := fmt.Sprintf(stmtFormat, strings.Join(placeholders, ","))
		if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(stmt), append(batchArgs, args...)...); err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// OverviewMergedPRRow is one change request merged inside the window. Only rows
// that name a workspace reach this read, so a workspace with no code-host
// connection contributes none rather than contributing a row it cannot own.
type OverviewMergedPRRow struct {
	WorkspaceID string `db:"workspace_id"`
	TaskID      string `db:"task_id"`
	Owner       string `db:"owner"`
	Repo        string `db:"repo"`
	Number      int    `db:"pr_number"`
	Title       string `db:"title"`
	MergedAtRaw string `db:"merged_at"`
	MergedAt    time.Time
}

// ListOverviewMergedPRs returns the change requests merged since `since` in the
// supplied workspaces, newest first, up to limit.
func (r *Repository) ListOverviewMergedPRs(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) ([]*OverviewMergedPRRow, error) {
	out, err := readOverviewBatched[OverviewMergedPRRow](ctx, r, workspaceIDs, `
		SELECT workspace_id, COALESCE(task_id, '') AS task_id, owner, repo, pr_number,
		       COALESCE(pr_title, '') AS title, CAST(merged_at AS TEXT) AS merged_at
		FROM github_task_prs
		WHERE workspace_id IN (%s)
		  AND merged_at IS NOT NULL AND merged_at >= ?
		ORDER BY merged_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range out {
		row.MergedAt = parseSqliteTime(row.MergedAtRaw)
	}
	sortByTimeDesc(out, func(row *OverviewMergedPRRow) time.Time { return row.MergedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// OverviewAutomationFailureRow is one automation run that failed inside the
// window. ErrorMessage is the run's own text and is never translated.
type OverviewAutomationFailureRow struct {
	WorkspaceID  string `db:"workspace_id"`
	AutomationID string `db:"automation_id"`
	TaskID       string `db:"task_id"`
	Title        string `db:"title"`
	ErrorMessage string `db:"error_message"`
	FailedAtRaw  string `db:"failed_at"`
	FailedAt     time.Time
}

// ListOverviewAutomationFailures returns the automation runs that failed since
// `since` in the supplied workspaces, newest first, up to limit. The workspace
// comes from the automation the run belongs to, so a run is never reported
// under a workspace it does not own.
func (r *Repository) ListOverviewAutomationFailures(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) ([]*OverviewAutomationFailureRow, error) {
	out, err := readOverviewBatched[OverviewAutomationFailureRow](ctx, r, workspaceIDs, `
		SELECT a.workspace_id, r.automation_id, COALESCE(r.task_id, '') AS task_id,
		       COALESCE(r.display_title, '') AS title,
		       SUBSTR(COALESCE(r.error_message, ''), 1, 300) AS error_message,
		       CAST(r.created_at AS TEXT) AS failed_at
		FROM automation_runs r
		JOIN automations a ON a.id = r.automation_id
		WHERE a.workspace_id IN (%s)
		  AND LOWER(COALESCE(r.status, '')) = 'failed'
		  AND r.created_at >= ?
		ORDER BY r.created_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range out {
		row.FailedAt = parseSqliteTime(row.FailedAtRaw)
	}
	sortByTimeDesc(out, func(row *OverviewAutomationFailureRow) time.Time { return row.FailedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// OverviewDecisionRow is one thing a person was asked to decide inside the
// window. AskedAt is always present; DecidedAt is present only once somebody
// answered, which is what separates an open question from an answered one.
// An approval names no task, so no task is reported for it.
type OverviewDecisionRow struct {
	WorkspaceID  string `db:"workspace_id"`
	Type         string `db:"type"`
	AskedAtRaw   string `db:"asked_at"`
	DecidedAtRaw string `db:"decided_at"`
	AskedAt      time.Time
	DecidedAt    time.Time
}

// ListOverviewDecisions returns the decisions raised since `since` in the
// supplied workspaces, newest first, up to limit. Both instants come from one
// approval row rather than from correlating a question list against an answer
// list, so an asked-but-unanswered question cannot lose its answer.
func (r *Repository) ListOverviewDecisions(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) ([]*OverviewDecisionRow, error) {
	out, err := readOverviewBatched[OverviewDecisionRow](ctx, r, workspaceIDs, `
		SELECT workspace_id, COALESCE(type, '') AS type,
		       CAST(created_at AS TEXT) AS asked_at,
		       CAST(decided_at AS TEXT) AS decided_at
		FROM office_approvals
		WHERE workspace_id IN (%s)
		  AND created_at >= ?
		ORDER BY created_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range out {
		row.AskedAt = parseSqliteTime(row.AskedAtRaw)
		row.DecidedAt = parseSqliteTime(row.DecidedAtRaw)
	}
	sortByTimeDesc(out, func(row *OverviewDecisionRow) time.Time { return row.AskedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// OverviewStepTransitionRow is one committed workflow-step change. The
// destination step's own configuration is joined in rather than inferred from
// its name, so whether arriving starts work is read from the same evidence
// every other consumer of that question reads.
type OverviewStepTransitionRow struct {
	TaskID        string `db:"task_id"`
	WorkspaceID   string `db:"workspace_id"`
	Title         string `db:"title"`
	StepID        string `db:"step_id"`
	StepName      string `db:"step_name"`
	StepEvents    string `db:"step_events"`
	PullFromStep  string `db:"pull_from_step_id"`
	Trigger       string `db:"trigger"`
	ActorKind     string `db:"actor_kind"`
	OccurredAtRaw string `db:"occurred_at"`
	OccurredAt    time.Time
}

// RunsOnEntry reports whether arriving at this row's step starts work by
// itself, read from the step's configuration through the shared decision rather
// than from a name or a workflow. A step whose events cannot be read is treated
// as starting nothing, which reports the move as stopped rather than inventing
// progress.
func (r *OverviewStepTransitionRow) RunsOnEntry() bool {
	var events wfmodels.StepEvents
	if r.StepEvents != "" {
		if err := json.Unmarshal([]byte(r.StepEvents), &events); err != nil {
			events = wfmodels.StepEvents{}
		}
	}
	return wfmodels.StepAutomation{
		OnEnter:        events.OnEnter,
		PullFromStepID: r.PullFromStep,
	}.RunsOnEntry()
}

// ListOverviewStepTransitions returns the step changes since `since` for the
// supplied workspaces, up to limit, ordered by task and time so a caller can
// group a task's run of moves without a second sort. Task creation is left out
// because the new-task source already reports it; what remains is movement
// between steps.
func (r *Repository) ListOverviewStepTransitions(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) ([]*OverviewStepTransitionRow, error) {
	out, err := readOverviewBatched[OverviewStepTransitionRow](ctx, r, workspaceIDs, `
		SELECT tr.task_id, t.workspace_id, COALESCE(t.title, '') AS title,
		       COALESCE(tr.to_workflow_step_id, '') AS step_id,
		       COALESCE(ws.name, '') AS step_name,
		       COALESCE(ws.events, '') AS step_events,
		       COALESCE(ws.pull_from_step_id, '') AS pull_from_step_id,
		       COALESCE(tr.trigger, '') AS trigger,
		       COALESCE(tr.actor_kind, '') AS actor_kind,
		       CAST(tr.occurred_at AS TEXT) AS occurred_at
		FROM task_step_transitions tr
		JOIN tasks t ON t.id = tr.task_id
		LEFT JOIN workflow_steps ws ON ws.id = tr.to_workflow_step_id
		WHERE t.workspace_id IN (%s)
		  AND COALESCE(tr.trigger, '') <> 'task_created'
		  AND tr.occurred_at >= ?
		ORDER BY tr.task_id, tr.occurred_at ASC
		LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range out {
		row.OccurredAt = parseSqliteTime(row.OccurredAtRaw)
	}
	return out, nil
}

// OverviewFailureFollowupRow is what one failed session was followed by on its
// own task: the first later session and how many later sessions exist, the
// routing reason and attempt count that session recorded, and the task's state
// now. LaterSessions counts the sessions started after this failure, so a task
// that churned through several is not reported as a single retry.
type OverviewFailureFollowupRow struct {
	TaskID      string `db:"task_id"`
	TaskState   string `db:"task_state"`
	SessionID   string `db:"session_id"`
	FailedAtRaw string `db:"failed_at"`
	// FailedAt is FailedAtRaw parsed. The reader parses every timestamp it
	// returns, so no caller has to know which shape the driver wrote.
	FailedAt time.Time

	NextSessionID   string `db:"next_session_id"`
	NextProfileID   string `db:"next_profile_id"`
	NextState       string `db:"next_state"`
	NextStartedRaw  string `db:"next_started_at"`
	NextStarted     time.Time
	LaterSessions   int    `db:"later_sessions"`
	NextRouteReason string `db:"next_route_reason"`
	RouteAttempts   int    `db:"route_attempts"`
}

// ListOverviewFailureFollowups returns one row per session that failed since
// `since`, newest failure first, up to limit. It reads every failed session of
// the supplied workspaces rather than only the tasks the open-task query already
// returned, because a task whose only failure happened after it closed is
// exactly the case that read would miss.
func (r *Repository) ListOverviewFailureFollowups(
	ctx context.Context, workspaceIDs []string, since time.Time, limit int,
) ([]*OverviewFailureFollowupRow, error) {
	out, err := readOverviewBatched[OverviewFailureFollowupRow](ctx, r, workspaceIDs, `
		SELECT f.task_id, COALESCE(t.state, '') AS task_state, f.id AS session_id,
		       CAST(f.updated_at AS TEXT) AS failed_at,
		       COALESCE(n.id, '') AS next_session_id,
		       COALESCE(n.agent_profile_id, '') AS next_profile_id,
		       COALESCE(n.state, '') AS next_state,
		       CAST(n.started_at AS TEXT) AS next_started_at,
		       (SELECT COUNT(*) FROM task_sessions s
		         WHERE s.task_id = f.task_id AND s.started_at > f.started_at) AS later_sessions,
		       COALESCE(n.route_reason, '') AS next_route_reason,
		       (SELECT COUNT(*) FROM dynamic_route_attempts d
		         WHERE d.session_id = n.id) AS route_attempts
		FROM task_sessions f
		JOIN tasks t ON t.id = f.task_id
		LEFT JOIN task_sessions n ON n.id = (
			SELECT s.id FROM task_sessions s
			WHERE s.task_id = f.task_id AND s.started_at > f.started_at
			ORDER BY s.started_at ASC LIMIT 1
		)
		WHERE t.workspace_id IN (%s)
		  AND f.state = 'FAILED'
		  AND f.updated_at >= ?
		ORDER BY f.updated_at DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	for _, row := range out {
		row.NextStarted = parseSqliteTime(row.NextStartedRaw)
		row.FailedAt = parseSqliteTime(row.FailedAtRaw)
	}
	sortByTimeDesc(out, func(row *OverviewFailureFollowupRow) time.Time { return row.FailedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
