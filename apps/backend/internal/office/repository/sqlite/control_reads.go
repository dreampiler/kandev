package sqlite

import (
	"context"
	"fmt"
	"time"
)

// ControlTask is a scoped projection of recorded state, not an inference signal.
type ControlTask struct {
	ID              string    `json:"task_id" db:"id"`
	WorkspaceID     string    `json:"workspace_id" db:"workspace_id"`
	Title           string    `json:"title" db:"title"`
	State           string    `json:"state" db:"state"`
	StepID          string    `json:"step_id" db:"step_id"`
	StepName        string    `json:"step_name" db:"step_name"`
	StageType       string    `json:"stage_type" db:"stage_type"`
	SessionID       string    `json:"session_id" db:"session_id"`
	SessionState    string    `json:"session_state" db:"session_state"`
	Archived        bool      `json:"archived" db:"archived"`
	RunningSessions int       `json:"running_sessions" db:"running_sessions"`
	QueuedMessages  int       `json:"queued_messages" db:"queued_messages"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

type ControlTaskList struct {
	Kind        string        `json:"kind"`
	Filter      string        `json:"filter"`
	Archive     string        `json:"archive"`
	Total       int           `json:"total"`
	Truncated   bool          `json:"truncated"`
	GeneratedAt time.Time     `json:"generated_at"`
	Tasks       []ControlTask `json:"tasks"`
}

const controlTaskFrom = ` FROM tasks t LEFT JOIN workflow_steps w ON w.id=t.workflow_step_id `
const controlTaskScope = `t.workspace_id=? AND t.is_ephemeral=0 AND ` + notAutomationOriginT
const controlPrimarySession = `(SELECT s.id FROM task_sessions s WHERE s.task_id=t.id AND s.is_primary=1 ORDER BY s.started_at DESC,s.id DESC LIMIT 1)`
const controlRunning = `(SELECT COUNT(*) FROM task_sessions s WHERE s.task_id=t.id AND s.state='RUNNING')`
const controlQueued = `(SELECT COUNT(*) FROM queued_messages q WHERE q.task_id=t.id)`

func controlTaskPredicate(filter, archive string, now time.Time) (string, []any, error) {
	where := controlTaskScope
	switch archive {
	case "active":
		where += " AND t.archived_at IS NULL"
	case "archived":
		where += " AND t.archived_at IS NOT NULL"
	case "all":
	default:
		return "", nil, fmt.Errorf("invalid archive selection")
	}
	condition, args, err := controlTaskFilter(filter, now)
	return where + " AND (" + condition + ")", args, err
}

func controlTaskFilter(filter string, now time.Time) (string, []any, error) {
	switch filter {
	case "all":
		return "1=1", nil, nil
	case "active":
		return "t.state IN ('IN_PROGRESS','REVIEW','SCHEDULING')", nil, nil
	case "sessions":
		return controlRunning + ">0", nil, nil
	case "queued":
		return controlQueued + ">0", nil, nil
	case "hold":
		return "(t.state='BLOCKED' OR COALESCE(w.stage_type,'')='hold')", nil, nil
	case "problems":
		return "(t.state='FAILED' OR EXISTS (SELECT 1 FROM task_sessions s WHERE s.id=" + controlPrimarySession + " AND s.state='FAILED'))", nil, nil
	case "completed":
		return `EXISTS (SELECT 1 FROM task_step_transitions x JOIN workflow_steps dest ON dest.id=x.to_workflow_step_id WHERE x.task_id=t.id AND dest.complete_task_on_enter=1 AND x.occurred_at>=? AND x.occurred_at<=?)`, []any{now.Add(-24 * time.Hour), now}, nil
	default:
		return "", nil, fmt.Errorf("invalid task filter")
	}
}

// ListControlTasks counts matches before clipping. Both reads use the existing
// read pool; generated_at anchors the time window, not an atomic DB snapshot.
func (r *Repository) ListControlTasks(ctx context.Context, wsID, filter, archive string, limit int, now time.Time) (*ControlTaskList, error) {
	where, extra, err := controlTaskPredicate(filter, archive, now)
	if err != nil {
		return nil, err
	}
	args := append([]any{wsID}, extra...)
	out := &ControlTaskList{Kind: "tasks", Filter: filter, Archive: archive, GeneratedAt: now, Tasks: []ControlTask{}}
	if err = r.ro.GetContext(ctx, &out.Total, r.ro.Rebind("SELECT COUNT(*)"+controlTaskFrom+"WHERE "+where), args...); err != nil {
		return nil, err
	}
	query := `SELECT t.id,t.workspace_id,t.title,COALESCE(t.state,'') AS state,COALESCE(w.id,'') AS step_id,COALESCE(w.name,'') AS step_name,COALESCE(w.stage_type,'') AS stage_type,
	 COALESCE(` + controlPrimarySession + `, '') AS session_id,
	 COALESCE((SELECT s.state FROM task_sessions s WHERE s.id=` + controlPrimarySession + `),'') AS session_state,
	 (t.archived_at IS NOT NULL) AS archived,` + controlRunning + ` AS running_sessions,` + controlQueued + ` AS queued_messages,t.updated_at` + controlTaskFrom + `WHERE ` + where + ` ORDER BY t.updated_at DESC,t.id DESC`
	if limit != -1 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if err = r.ro.SelectContext(ctx, &out.Tasks, r.ro.Rebind(query), args...); err != nil {
		return nil, err
	}
	out.Truncated = out.Total > len(out.Tasks)
	return out, nil
}
