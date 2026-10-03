package sqlite

import (
	"context"
	"fmt"
	"time"
)

// UsageLimitObservation is the recorded usage of a provider account at the
// moment one of its candidates hit a usage or rate limit. Kandev keeps it so the
// point where an undisclosed limit bites can be estimated from its own ledger.
// Values are recorded-only lower bounds over trailing windows ending at the hit.
type UsageLimitObservation struct {
	ID                 string    `db:"id"`
	ExecutionProfileID string    `db:"execution_profile_id"`
	AccountKey         string    `db:"account_key"`
	Code               string    `db:"code"`
	ObservedAt         time.Time `db:"observed_at"`
	Turns5h            int64     `db:"turns_5h"`
	Tokens5h           int64     `db:"tokens_5h"`
	TurnsDay           int64     `db:"turns_day"`
	TokensDay          int64     `db:"tokens_day"`
	CostSubcentsDay    int64     `db:"cost_subcents_day"`
	TurnsWeek          int64     `db:"turns_week"`
	TokensWeek         int64     `db:"tokens_week"`
}

// initUsageLimitObservationsSchema creates the limit observation log and the
// route attempt index that round-robin continuation reads. Both are new, so
// CREATE ... IF NOT EXISTS is complete.
func (r *Repository) initUsageLimitObservationsSchema() error {
	_, err := r.db.ExecContext(r.migrationContext(), `
		CREATE TABLE IF NOT EXISTS usage_limit_observations (
			id TEXT PRIMARY KEY,
			execution_profile_id TEXT NOT NULL,
			account_key TEXT NOT NULL DEFAULT '',
			code TEXT NOT NULL DEFAULT '',
			observed_at TIMESTAMP NOT NULL,
			turns_5h BIGINT NOT NULL DEFAULT 0,
			tokens_5h BIGINT NOT NULL DEFAULT 0,
			turns_day BIGINT NOT NULL DEFAULT 0,
			tokens_day BIGINT NOT NULL DEFAULT 0,
			cost_subcents_day BIGINT NOT NULL DEFAULT 0,
			turns_week BIGINT NOT NULL DEFAULT 0,
			tokens_week BIGINT NOT NULL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_usage_limit_observations_account
			ON usage_limit_observations(account_key, observed_at);

		CREATE INDEX IF NOT EXISTS idx_dynamic_route_attempts_logical_created
			ON dynamic_route_attempts(logical_profile_id, created_at);
	`)
	if err != nil {
		return fmt.Errorf("create usage limit observations: %w", err)
	}
	return nil
}

// InsertUsageLimitObservation appends one observation.
func (r *Repository) InsertUsageLimitObservation(ctx context.Context, observation UsageLimitObservation) error {
	_, err := r.db.ExecContext(ctx, r.db.Rebind(`
		INSERT INTO usage_limit_observations (
			id, execution_profile_id, account_key, code, observed_at,
			turns_5h, tokens_5h, turns_day, tokens_day, cost_subcents_day, turns_week, tokens_week
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`),
		observation.ID, observation.ExecutionProfileID, observation.AccountKey, observation.Code,
		observation.ObservedAt.UTC(),
		observation.Turns5h, observation.Tokens5h, observation.TurnsDay, observation.TokensDay,
		observation.CostSubcentsDay, observation.TurnsWeek, observation.TokensWeek,
	)
	return err
}

// ListUsageLimitObservationsSince returns observations at or after since,
// newest first.
func (r *Repository) ListUsageLimitObservationsSince(
	ctx context.Context,
	since time.Time,
) ([]UsageLimitObservation, error) {
	var observations []UsageLimitObservation
	err := r.ro.SelectContext(ctx, &observations, r.ro.Rebind(`
		SELECT id, execution_profile_id, account_key, code, observed_at,
		       turns_5h, tokens_5h, turns_day, tokens_day, cost_subcents_day, turns_week, tokens_week
		  FROM usage_limit_observations
		 WHERE observed_at >= ?
		 ORDER BY observed_at DESC
	`), since.UTC())
	return observations, err
}

// LastDynamicRouteSelections returns when each concrete candidate of a logical
// dynamic profile was last selected, from the durable route attempt log. The
// scan is bounded to recent attempts because only the latest per candidate
// matters.
func (r *Repository) LastDynamicRouteSelections(
	ctx context.Context,
	logicalProfileID string,
) (map[string]time.Time, error) {
	rows, err := r.ro.QueryxContext(ctx, r.ro.Rebind(`
		SELECT execution_profile_id, created_at
		  FROM dynamic_route_attempts
		 WHERE logical_profile_id = ? AND execution_profile_id <> ''
		 ORDER BY created_at DESC
		 LIMIT 500
	`), logicalProfileID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	latest := make(map[string]time.Time)
	for rows.Next() {
		var candidateID string
		var createdAt time.Time
		if err := rows.Scan(&candidateID, &createdAt); err != nil {
			return nil, err
		}
		if _, seen := latest[candidateID]; !seen {
			latest[candidateID] = createdAt
		}
	}
	return latest, rows.Err()
}
