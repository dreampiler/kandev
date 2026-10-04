package sqlite

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
)

// ListAdmittedSessionRefs returns every session in the admitted population —
// state STARTING or RUNNING — across the entire instance: every workspace, every
// workflow, every board, Office and non-Office alike.
//
// The query deliberately has no joins and no filters. A session counts regardless
// of whether its task is archived, ephemeral or automation-origin, and regardless
// of config_mode or IsPassthrough on the session itself: each one runs an agent
// process and consumes a core. Adding any of the filters the neighbouring task
// queries carry would under-count exactly the work the ceiling exists to bound.
// The agent profile id is read from the session row itself, which is where the
// launch already recorded it, so no join can change which sessions are counted.
//
// The state literals here are written out rather than derived from
// models.IsAdmittedSessionState on purpose: deriving them would make the
// drift-guard test compare the predicate against itself. Keeping the two sides
// independent is what lets TestAdmittedSessionIDsMatchSQLFilter catch an edit to
// one that was not made to the other.
func (r *Repository) ListAdmittedSessionRefs(ctx context.Context) ([]models.AdmittedSessionRef, error) {
	rows, err := r.ro.QueryContext(ctx, r.ro.Rebind(`
		SELECT s.id, COALESCE(s.agent_profile_id, '')
		FROM task_sessions s
		WHERE s.state IN (?, ?)
		ORDER BY s.id ASC
	`), string(models.TaskSessionStateStarting), string(models.TaskSessionStateRunning))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var refs []models.AdmittedSessionRef
	for rows.Next() {
		var ref models.AdmittedSessionRef
		if err := rows.Scan(&ref.ID, &ref.ProfileID); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}
