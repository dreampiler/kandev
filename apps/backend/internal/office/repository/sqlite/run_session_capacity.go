package sqlite

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/office/models"
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
