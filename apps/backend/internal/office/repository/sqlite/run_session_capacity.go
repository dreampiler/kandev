package sqlite

import (
	"context"
	"maps"
	"time"

	"github.com/kandev/kandev/internal/office/models"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

// RunSessionCapacityBlockedStatus is distinct from provider and operator blocks.
const RunSessionCapacityBlockedStatus = "blocked_session_capacity"

// SetRunSessionCapacityReleased wires the existing release-driven retry driver
// before launch consumers start. The callback runs only after a terminal CAS.
func (r *Repository) SetRunSessionCapacityReleased(notify func()) {
	r.runSessionCapacityReleased = notify
}

func (r *Repository) ListAdmittedRunSessionIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.ro.SelectContext(ctx, &ids, r.ro.Rebind(`
		SELECT id FROM office_run_sessions WHERE state IN (?, ?) ORDER BY id
	`), models.RunSessionStatePreparing, models.RunSessionStateRunning)
	return ids, err
}

// ListAdmittedRunExecutions carries the grade frozen on each run attempt.
func (r *Repository) ListAdmittedRunExecutions(ctx context.Context) ([]taskmodels.AdmittedExecution, error) {
	var rows []taskmodels.AdmittedExecution
	err := r.ro.SelectContext(ctx, &rows, r.ro.Rebind(`SELECT id, admitted_execution_grade FROM office_run_sessions WHERE state IN (?, ?) ORDER BY id`), models.RunSessionStatePreparing, models.RunSessionStateRunning)
	return rows, err
}

// ParkRunForSessionCapacity returns only an unlaunched claimed taskless run to
// its queue. It retains priority, requested time, route, identity and retry state.
func (r *Repository) ParkRunForSessionCapacity(ctx context.Context, runID string, claimedAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, r.db.Rebind(`
		UPDATE runs SET status = 'queued', claimed_at = NULL,
		    routing_blocked_status = ?
		WHERE id = ? AND status = 'claimed'
		  AND (claimed_at = ? OR (claimed_at IS NULL AND ? IS NULL))
		  AND routing_blocked_status IS NULL
		  AND NOT EXISTS (SELECT 1 FROM office_run_sessions s
		      WHERE s.run_id = runs.id AND s.state IN (?, ?))
	`), RunSessionCapacityBlockedStatus, runID, claimedAt, claimedAt,
		models.RunSessionStatePreparing, models.RunSessionStateRunning)
	return err
}

// ResumeSessionCapacityRuns makes only capacity-blocked queued runs eligible
// for the existing scheduler. Zero means the install ceiling is disabled.
func (r *Repository) ResumeSessionCapacityRuns(ctx context.Context, available int) error {
	if available == 0 {
		available = 2147483647
	}
	_, err := r.db.ExecContext(ctx, r.db.Rebind(`
		UPDATE runs SET routing_blocked_status = NULL
		WHERE status = 'queued' AND routing_blocked_status = ? AND id IN (
		    SELECT q.id FROM runs q
		    WHERE q.status = 'queued' AND q.routing_blocked_status = ?
		      AND NOT EXISTS (SELECT 1 FROM office_run_sessions s
		          WHERE s.run_id = q.id AND s.state IN (?, ?))
		    ORDER BY q.priority_class, q.requested_at, q.id LIMIT ?
		)
	`), RunSessionCapacityBlockedStatus, RunSessionCapacityBlockedStatus,
		models.RunSessionStatePreparing, models.RunSessionStateRunning, available)
	return err
}

// ResumeExecutionCapacityRuns reuses the capacity queue without letting a full grade
// hide eligible work in another grade. Actual launch still reserves atomically.
func (r *Repository) ResumeExecutionCapacityRuns(ctx context.Context, available int, gradeAvailable map[string]int) error {
	var candidates []struct {
		ID    string `db:"id"`
		Grade string `db:"grade"`
	}
	err := r.ro.SelectContext(ctx, &candidates, r.ro.Rebind(`
		SELECT q.id, COALESCE(logical.execution_grade, identity.execution_grade, '') AS grade
		FROM runs q
		JOIN agent_profiles identity ON identity.id = q.agent_profile_id
		LEFT JOIN agent_profiles logical ON logical.id = NULLIF(identity.execution_agent_profile_id, '')
		WHERE q.status = 'queued' AND q.routing_blocked_status = ?
		  AND NOT EXISTS (SELECT 1 FROM office_run_sessions s WHERE s.run_id = q.id AND s.state IN (?, ?))
		ORDER BY q.priority_class, q.requested_at, q.id
	`), RunSessionCapacityBlockedStatus, models.RunSessionStatePreparing, models.RunSessionStateRunning)
	if err != nil {
		return err
	}
	remaining := maps.Clone(gradeAvailable)
	for _, candidate := range candidates {
		if slots, limited := remaining[candidate.Grade]; limited && slots <= 0 {
			continue
		}
		result, err := r.db.ExecContext(ctx, r.db.Rebind(`UPDATE runs SET routing_blocked_status = NULL WHERE id = ? AND status = 'queued' AND routing_blocked_status = ? AND NOT EXISTS (SELECT 1 FROM office_run_sessions s WHERE s.run_id = runs.id AND s.state IN (?, ?))`), candidate.ID, RunSessionCapacityBlockedStatus, models.RunSessionStatePreparing, models.RunSessionStateRunning)
		if err != nil {
			return err
		}
		wrote, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if wrote == 0 {
			continue
		}
		if _, limited := remaining[candidate.Grade]; limited {
			remaining[candidate.Grade]--
		}
		if available > 0 {
			available--
			if available == 0 {
				break
			}
		}
	}
	return nil
}
