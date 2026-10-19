package sqlite

import (
	"context"
	"time"
)

type ControlMetrics struct {
	WorkspaceID       string         `json:"workspace_id"`
	GeneratedAt       time.Time      `json:"generated_at"`
	Since             time.Time      `json:"since"`
	Basis             string         `json:"basis"`
	InferenceState    string         `json:"inference_state"`
	Tasks             map[string]int `json:"tasks"`
	Sessions          map[string]int `json:"sessions"`
	QueuedMessages    int            `json:"queued_messages"`
	Transitions24h    int            `json:"transitions_24h"`
	CompletedTasks24h int            `json:"completed_tasks_24h"`
}

func (r *Repository) ReadControlMetrics(ctx context.Context, wsID string, now time.Time) (*ControlMetrics, error) {
	out := &ControlMetrics{WorkspaceID: wsID, GeneratedAt: now, Since: now.Add(-24 * time.Hour), Basis: "recorded_task_session_queue_transition_state", InferenceState: "unknown"}
	var err error
	out.Tasks, err = r.controlStateCounts(ctx, "SELECT COALESCE(t.state,'') AS state,COUNT(*) AS count FROM tasks t WHERE "+controlTaskScope+" AND t.archived_at IS NULL GROUP BY t.state", wsID)
	if err != nil {
		return nil, err
	}
	out.Sessions, err = r.controlStateCounts(ctx, "SELECT s.state,COUNT(*) AS count FROM task_sessions s JOIN tasks t ON t.id=s.task_id WHERE "+controlTaskScope+" AND t.archived_at IS NULL GROUP BY s.state", wsID)
	if err != nil {
		return nil, err
	}
	queries := []struct {
		dest *int
		sql  string
		args []any
	}{
		{&out.QueuedMessages, "SELECT COUNT(*) FROM queued_messages q JOIN tasks t ON t.id=q.task_id WHERE " + controlTaskScope + " AND t.archived_at IS NULL", []any{wsID}},
		{&out.Transitions24h, "SELECT COUNT(*) FROM task_step_transitions x JOIN tasks t ON t.id=x.task_id WHERE " + controlTaskScope + " AND x.occurred_at>=? AND x.occurred_at<=?", []any{wsID, out.Since, now}},
		{&out.CompletedTasks24h, "SELECT COUNT(DISTINCT x.task_id) FROM task_step_transitions x JOIN tasks t ON t.id=x.task_id JOIN workflow_steps w ON w.id=x.to_workflow_step_id WHERE " + controlTaskScope + " AND w.complete_task_on_enter=1 AND x.occurred_at>=? AND x.occurred_at<=?", []any{wsID, out.Since, now}},
	}
	for _, q := range queries {
		if err = r.ro.GetContext(ctx, q.dest, r.ro.Rebind(q.sql), q.args...); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Repository) controlStateCounts(ctx context.Context, query, wsID string) (map[string]int, error) {
	rows := []struct {
		State string `db:"state"`
		Count int    `db:"count"`
	}{}
	if err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(query), wsID); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.State] = row.Count
	}
	return out, nil
}
