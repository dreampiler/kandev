package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/office/routing"
	runsmodels "github.com/kandev/kandev/internal/runs/models"
)

// The payload fields the launch-deferral disposition writes on the durable
// run event immediately before it parks the run. Two sites write them —
// scheduler/dispatch_routing.go and service/scheduler_integration.go — and
// nothing else in the codebase writes this payload.
const (
	sessionCeilingDeferralPhase  = "deferred"
	sessionCeilingDeferralReason = "session_ceiling"
)

// backfillSessionCeilingParkedRuns moves runs that a build from before the
// waiting_for_session_capacity park status existed left under
// blocked_provider_action_required onto that status with an automatic
// retry deadline, so the scheduler's timed lift (ListPendingProviderCapacityRuns
// feeding LiftParkedRuns) can return them to dispatch instead of leaving
// them queued until an operator clears the block by hand.
//
// The discriminating evidence is durable rather than inferred: the
// launch-deferral disposition appends an adapter.invoke run event carrying
// {"phase":"deferred","reason":"session_ceiling"} immediately before it
// parks the run. The dispositions that park under
// blocked_provider_action_required for a genuine provider fault
// (parkRunMaxAttempts, parkRunBlocked) record a route attempt and never
// append a run event, so a run whose latest event is the deferral marker is
// one the orchestrator's session ceiling deferred and nothing has happened
// to since. A run with any other latest event, or with no events at all,
// is left for an operator to settle.
//
// Runs once per boot from runMigrations. Idempotent by construction: a
// migrated row no longer matches the candidate filter, so a replay is a
// no-op and no guard column is required.
func (r *Repository) backfillSessionCeilingParkedRuns() error {
	runIDs, err := r.selectSessionCeilingParkCandidates()
	if err != nil {
		return err
	}
	if len(runIDs) == 0 {
		return nil
	}
	retryAt := time.Now().UTC().Add(routing.SessionCeilingRetryDelay)
	migrated := 0
	for _, runID := range runIDs {
		moved, err := r.migrateSessionCeilingParkedRun(runID, retryAt)
		if err != nil {
			return err
		}
		if moved {
			migrated++
		}
	}
	if r.log != nil && migrated > 0 {
		r.log.Info("session ceiling park backfill applied",
			zap.Int("migrated_runs", migrated),
			zap.Time("earliest_retry_at", retryAt))
	}
	return nil
}

// selectSessionCeilingParkCandidates returns the queued runs still parked
// under blocked_provider_action_required. Only the id is read so the scan
// stays narrow on a run-history table.
func (r *Repository) selectSessionCeilingParkCandidates() ([]string, error) {
	var runIDs []string
	if err := r.db.Select(&runIDs, r.db.Rebind(`
		SELECT id FROM runs
		WHERE routing_blocked_status = ?
		  AND status = 'queued'
	`), runsmodels.RoutingBlockedActionRequired); err != nil {
		return nil, fmt.Errorf("session ceiling park backfill: list candidates: %w", err)
	}
	return runIDs, nil
}

// migrateSessionCeilingParkedRun moves one candidate run when its latest
// run event is the session-ceiling deferral marker, and reports whether the
// row was actually updated. The status predicate in the UPDATE keeps the
// write scoped to a row nobody else has re-parked since the candidate read.
func (r *Repository) migrateSessionCeilingParkedRun(runID string, retryAt time.Time) (bool, error) {
	var event struct {
		EventType string `db:"event_type"`
		Payload   string `db:"payload"`
	}
	err := r.db.Get(&event, r.db.Rebind(`
		SELECT event_type, payload FROM run_events
		WHERE run_id = ? ORDER BY seq DESC LIMIT 1
	`), runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("session ceiling park backfill: latest event for %q: %w", runID, err)
	}
	if event.EventType != string(runsmodels.RunEventTypeAdapterInvoke) ||
		!isSessionCeilingDeferralPayload(event.Payload) {
		return false, nil
	}
	result, err := r.db.Exec(r.db.Rebind(`
		UPDATE runs
		SET routing_blocked_status = ?,
		    earliest_retry_at = ?,
		    scheduled_retry_at = ?
		WHERE id = ?
		  AND routing_blocked_status = ?
	`), runsmodels.RoutingBlockedWaitingForSessionCapacity, retryAt, retryAt,
		runID, runsmodels.RoutingBlockedActionRequired)
	if err != nil {
		return false, fmt.Errorf("session ceiling park backfill: update %q: %w", runID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("session ceiling park backfill: rows affected for %q: %w", runID, err)
	}
	return affected > 0, nil
}

// isSessionCeilingDeferralPayload reports whether a run event payload is the
// marker the launch-deferral disposition wrote. The payload is parsed in Go
// rather than through a JSON SQL function so the migration stays portable
// across the SQLite and PostgreSQL engines the office store runs on.
func isSessionCeilingDeferralPayload(payload string) bool {
	var body struct {
		Phase  string `json:"phase"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		return false
	}
	return body.Phase == sessionCeilingDeferralPhase && body.Reason == sessionCeilingDeferralReason
}
