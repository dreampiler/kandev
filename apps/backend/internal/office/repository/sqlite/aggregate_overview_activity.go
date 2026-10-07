package sqlite

import (
	"context"
	"strings"
	"time"
)

// Read-only per-workspace activity statistics for the overview's project
// statistics block. Every count is a plain aggregate over existing tables: no
// new storage, no row cap (the numbers are exact totals, not a sample), and no
// dialect-specific JSON. The caller supplies the look-back window, so one pair
// of reads answers 24 hours, 7 days, or 30 days.

// OverviewWorkspaceActivityCounts is one workspace's period totals.
type OverviewWorkspaceActivityCounts struct {
	WorkspaceID     string
	SessionsStarted int
	SessionsFailed  int
	AgentTurns      int
	StepMoves       int
	Completed       int
}

// OverviewFailureBucketRow is one grouped count of failed sessions: the
// workspace, the coarse reason bucket the session fell in, the session's own
// error text (empty when it recorded none), and how many sessions shared that
// exact text. The text is kept so a caller can show the agent's own words under
// the bucket rather than only a taxonomy label.
type OverviewFailureBucketRow struct {
	WorkspaceID  string `db:"workspace_id"`
	Bucket       string `db:"bucket"`
	ErrorMessage string `db:"error_message"`
	Count        int    `db:"cnt"`
}

// The four failure buckets, matching the wire codes the dashboard exposes. They
// are the owner's taxonomy: a session that answered nothing, one stopped by a
// provider limit, one that never ran, and anything else.
const (
	overviewFailureBucketNoResponse  = "no_response"
	overviewFailureBucketLimit       = "limit"
	overviewFailureBucketStartFailed = "start_failed"
	overviewFailureBucketOther       = "other"
)

// overviewLimitSignals are the lowercased substrings that mark a failure as a
// provider usage limit rather than a fault in the run. They mirror the provider
// error classifier's own vocabulary (rate limits, quota, model capacity,
// resource exhaustion) so the bucket agrees with the routing decision.
var overviewLimitSignals = []string{
	"rate limit", "rate_limit", "ratelimit", "too many requests",
	"quota", "credit balance", "insufficient credits", "usage limit",
	"at capacity", "model_capacity", "capacity_reached", "overloaded",
	"resource_exhausted", "resource exhausted", "429", "529",
}

// overviewStartSignals are the lowercased substrings that mark a failure to
// launch. A session that never completed a turn is treated the same way: it
// ended before it ran, whatever its message says.
var overviewStartSignals = []string{
	"failed to start", "startup", "spawn", "command not found",
	"no such file", "not installed", "permission denied", "eacces",
	"exit code 127",
}

// ListOverviewWorkspaceActivity returns the period totals per workspace:
// sessions started, sessions that failed, agent turns, step moves, and
// completed tasks. Every count is bounded by the caller's window; automation
// runs and ephemeral tasks are excluded so the figures line up with the rest of
// the overview.
func (r *Repository) ListOverviewWorkspaceActivity(
	ctx context.Context, workspaceIDs []string, since time.Time,
) (map[string]OverviewWorkspaceActivityCounts, error) {
	out := make(map[string]OverviewWorkspaceActivityCounts, len(workspaceIDs))
	sessions, err := readOverviewBatched[overviewSessionActivityRow](ctx, r, workspaceIDs, `
		SELECT t.workspace_id AS workspace_id,
		       COUNT(*) AS started,
		       SUM(CASE WHEN s.state = 'FAILED' THEN 1 ELSE 0 END) AS failed
		FROM task_sessions s
		JOIN tasks t ON t.id = s.task_id
		WHERE t.workspace_id IN (%s)
		  AND t.is_ephemeral = 0`+andNotAutomationOriginT+`
		  AND s.started_at >= ?
		GROUP BY t.workspace_id`, since)
	if err != nil {
		return nil, err
	}
	for _, row := range sessions {
		c := out[row.WorkspaceID]
		c.WorkspaceID = row.WorkspaceID
		c.SessionsStarted = row.Started
		c.SessionsFailed = row.Failed
		out[row.WorkspaceID] = c
	}
	turns, err := readOverviewBatched[overviewActivityCountRow](ctx, r, workspaceIDs, `
		SELECT t.workspace_id AS workspace_id, COUNT(*) AS cnt
		FROM task_session_turns tn
		JOIN tasks t ON t.id = tn.task_id
		WHERE t.workspace_id IN (%s)
		  AND t.is_ephemeral = 0`+andNotAutomationOriginT+`
		  AND tn.started_at >= ?
		GROUP BY t.workspace_id`, since)
	if err != nil {
		return nil, err
	}
	for _, row := range turns {
		c := out[row.WorkspaceID]
		c.WorkspaceID = row.WorkspaceID
		c.AgentTurns = row.Count
		out[row.WorkspaceID] = c
	}
	moves, err := readOverviewBatched[overviewActivityCountRow](ctx, r, workspaceIDs, `
		SELECT t.workspace_id AS workspace_id, COUNT(*) AS cnt
		FROM task_step_transitions tr
		JOIN tasks t ON t.id = tr.task_id
		WHERE t.workspace_id IN (%s)
		  AND t.is_ephemeral = 0`+andNotAutomationOriginT+`
		  AND COALESCE(tr.trigger, '') <> 'task_created'
		  AND tr.occurred_at >= ?
		GROUP BY t.workspace_id`, since)
	if err != nil {
		return nil, err
	}
	for _, row := range moves {
		c := out[row.WorkspaceID]
		c.WorkspaceID = row.WorkspaceID
		c.StepMoves = row.Count
		out[row.WorkspaceID] = c
	}
	completed, err := readOverviewBatched[overviewActivityCountRow](ctx, r, workspaceIDs, `
		SELECT t.workspace_id AS workspace_id, COUNT(DISTINCT t.id) AS cnt
		FROM tasks t
		JOIN task_step_transitions tr ON tr.task_id = t.id
		JOIN workflow_steps ws ON ws.id = tr.to_workflow_step_id
		WHERE t.workspace_id IN (%s)
		  AND t.is_ephemeral = 0`+andNotAutomationOriginT+`
		  AND t.state = 'COMPLETED'
		  AND ws.complete_task_on_enter = 1
		  AND tr.occurred_at >= ?
		GROUP BY t.workspace_id`, since)
	if err != nil {
		return nil, err
	}
	for _, row := range completed {
		c := out[row.WorkspaceID]
		c.WorkspaceID = row.WorkspaceID
		c.Completed = row.Count
		out[row.WorkspaceID] = c
	}
	return out, nil
}

// ListOverviewFailureBuckets returns the failed sessions of the window grouped
// by workspace, reason bucket, and the session's own error text. Grouping by
// the text rather than only by the bucket keeps the sample a caller shows under
// a bucket, while the bucket still carries the exact total.
func (r *Repository) ListOverviewFailureBuckets(
	ctx context.Context, workspaceIDs []string, since time.Time,
) ([]*OverviewFailureBucketRow, error) {
	message := "LOWER(COALESCE(s.error_message, ''))"
	bucket := `CASE
			WHEN ` + overviewMessageLike(message, overviewLimitSignals) + ` THEN '` + overviewFailureBucketLimit + `'
			WHEN ` + overviewMessageLike(message, overviewStartSignals) + `
			     OR s.completed_at IS NULL OR s.completed_at <= s.started_at THEN '` + overviewFailureBucketStartFailed + `'
			WHEN COALESCE(TRIM(s.error_message), '') = '' THEN '` + overviewFailureBucketNoResponse + `'
			ELSE '` + overviewFailureBucketOther + `'
		END`
	return readOverviewBatched[OverviewFailureBucketRow](ctx, r, workspaceIDs, `
		SELECT t.workspace_id AS workspace_id,
		       `+bucket+` AS bucket,
		       SUBSTR(COALESCE(s.error_message, ''), 1, 300) AS error_message,
		       COUNT(*) AS cnt
		FROM task_sessions s
		JOIN tasks t ON t.id = s.task_id
		WHERE t.workspace_id IN (%s)
		  AND t.is_ephemeral = 0`+andNotAutomationOriginT+`
		  AND s.state = 'FAILED'
		  AND s.started_at >= ?
		GROUP BY t.workspace_id, bucket, error_message`, since)
}

// overviewMessageLike builds a parenthesized `col LIKE '%a%' OR ...` clause for
// the supplied lowercased substrings. The signals are fixed literals, so the
// values are inlined rather than bound; an empty list yields a false clause so
// the statement stays valid. The percent signs are doubled because the statement
// is expanded by readOverviewBatched's fmt.Sprintf, which would otherwise read
// each `%` as a format verb.
func overviewMessageLike(column string, signals []string) string {
	if len(signals) == 0 {
		return "1 = 0"
	}
	parts := make([]string, len(signals))
	for i, signal := range signals {
		parts[i] = column + " LIKE '%%" + signal + "%%'"
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// overviewSessionActivityRow is one workspace's session-started and
// session-failed totals from a single grouped read.
type overviewSessionActivityRow struct {
	WorkspaceID string `db:"workspace_id"`
	Started     int    `db:"started"`
	Failed      int    `db:"failed"`
}

// overviewActivityCountRow is one workspace's count from a grouped read.
type overviewActivityCountRow struct {
	WorkspaceID string `db:"workspace_id"`
	Count       int    `db:"cnt"`
}
