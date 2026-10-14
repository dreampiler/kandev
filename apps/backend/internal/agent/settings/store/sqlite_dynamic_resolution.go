package store

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/agent/settings/models"
)

var _ DynamicReferenceResolutionRepository = (*sqliteRepository)(nil)

// ResolveDynamicReferencesAndUpdateProfile replaces every referencing dynamic
// profile document and updates the target profile's enabled state in one
// transaction, so a version conflict or a failure at any step leaves the
// target row and every referencing document untouched.
func (r *sqliteRepository) ResolveDynamicReferencesAndUpdateProfile(
	ctx context.Context,
	target *models.AgentProfile,
	enabled *bool,
	resolutions []DynamicProfileResolution,
) error {
	if target == nil || target.ID == "" {
		return errors.New("target profile id is required")
	}
	return r.updateAgentProfileWithMembershipLocks(ctx, target, enabled, func(tx *sqlx.Tx) error {
		return r.resolveDynamicReferencesTx(ctx, tx, target.ID, resolutions)
	})
}

// ResolveDynamicReferencesAndDeleteProfile replaces every referencing dynamic
// profile document and soft-deletes the target profile in one transaction.
func (r *sqliteRepository) ResolveDynamicReferencesAndDeleteProfile(
	ctx context.Context,
	targetID string,
	resolutions []DynamicProfileResolution,
) error {
	if targetID == "" {
		return errors.New("target profile id is required")
	}
	return r.deleteAgentProfileTx(ctx, targetID, func(tx *sqlx.Tx) error {
		return r.resolveDynamicReferencesTx(ctx, tx, targetID, resolutions)
	})
}

// resolveDynamicReferencesTx re-reads the complete current reference set inside
// the transaction and verifies it exactly matches the snapshot the caller built
// the resolutions from, then replaces each referencing document under its
// optimistic version. A new or removed reference, or any version mismatch,
// returns ErrDynamicProfileVersionConflict so the whole transaction rolls back
// rather than partially updating or ignoring a reference.
func (r *sqliteRepository) resolveDynamicReferencesTx(
	ctx context.Context,
	tx *sqlx.Tx,
	targetID string,
	resolutions []DynamicProfileResolution,
) error {
	current, err := currentReferencingVersions(ctx, tx, targetID)
	if err != nil {
		return err
	}
	if len(current) != len(resolutions) {
		return ErrDynamicProfileVersionConflict
	}
	for _, resolution := range resolutions {
		version, ok := current[resolution.ProfileID]
		if !ok || version != resolution.ExpectedVersion {
			return ErrDynamicProfileVersionConflict
		}
	}
	for _, resolution := range resolutions {
		if err := r.replaceDynamicDocumentTx(ctx, tx, resolution); err != nil {
			return err
		}
	}
	return nil
}

func currentReferencingVersions(ctx context.Context, tx *sqlx.Tx, targetID string) (map[string]int64, error) {
	rows, err := tx.QueryxContext(ctx, tx.Rebind(`
		SELECT DISTINCT dp.profile_id, dp.version
		FROM dynamic_agent_routes r
		JOIN dynamic_agent_profiles dp ON dp.profile_id = r.dynamic_profile_id
		WHERE r.execution_profile_id = ?
	`), targetID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	versions := make(map[string]int64)
	for rows.Next() {
		var (
			id      string
			version int64
		)
		if err := rows.Scan(&id, &version); err != nil {
			return nil, err
		}
		versions[id] = version
	}
	return versions, rows.Err()
}

// replaceDynamicDocumentTx CASes the dynamic profile version and replaces its
// route rows in the caller's transaction, reusing the same statement as the
// ordinary dynamic update so the version bump and route replacement commit
// together.
func (r *sqliteRepository) replaceDynamicDocumentTx(
	ctx context.Context,
	tx *sqlx.Tx,
	resolution DynamicProfileResolution,
) error {
	profile := &models.DynamicAgentProfile{
		ProfileID:             resolution.ProfileID,
		KeepModelWhileRunning: resolution.KeepModelWhileRunning,
		Version:               resolution.ExpectedVersion,
	}
	return r.updateDynamicAgentProfileTx(ctx, tx, profile, resolution.ExpectedVersion, resolution.Routes)
}
