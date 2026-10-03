package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kandev/kandev/internal/agent/settings/models"
)

func newDynamicSelectionRepo(t *testing.T) (*sqliteRepository, *sqlx.DB, context.Context, *models.AgentProfile) {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo, err := newSQLiteRepository(db, db, nil, false)
	if err != nil {
		t.Fatalf("newSQLiteRepository: %v", err)
	}
	ctx := context.Background()
	agent := &models.Agent{ID: "dyn-sel", Name: "dyn-sel"}
	if err := repo.CreateAgent(ctx, agent); err != nil {
		t.Fatalf("create agent family: %v", err)
	}
	profile := &models.AgentProfile{AgentID: agent.ID, Name: "Tiered", AgentDisplayName: "Dynamic"}
	if err := repo.CreateAgentProfile(ctx, profile); err != nil {
		t.Fatalf("create dynamic profile: %v", err)
	}
	return repo, db, ctx, profile
}

func TestDynamicProfileKeepModelDefaultsTrue(t *testing.T) {
	repo, _, ctx, profile := newDynamicSelectionRepo(t)
	created := &models.DynamicAgentProfile{ProfileID: profile.ID, KeepModelWhileRunning: true}
	if err := repo.CreateDynamicAgentProfile(ctx, created, nil); err != nil {
		t.Fatalf("CreateDynamicAgentProfile: %v", err)
	}
	stored, _, err := repo.GetDynamicAgentProfile(ctx, profile.ID)
	if err != nil {
		t.Fatalf("GetDynamicAgentProfile: %v", err)
	}
	if !stored.KeepModelWhileRunning {
		t.Fatalf("keep model = false, want the documented default of true")
	}
}

func TestDynamicProfileKeepModelSurvivesClearedAndRepopulatedList(t *testing.T) {
	repo, _, ctx, profile := newDynamicSelectionRepo(t)
	created := &models.DynamicAgentProfile{ProfileID: profile.ID, KeepModelWhileRunning: false}
	if err := repo.CreateDynamicAgentProfile(ctx, created, []models.DynamicAgentRoute{
		{DynamicProfileID: profile.ID, Position: 0, ExecutionProfileID: "a", Enabled: true, RulesJSON: `{}`},
	}); err != nil {
		t.Fatalf("CreateDynamicAgentProfile: %v", err)
	}

	// Clearing the candidate list must not silently re-enable the preference.
	if err := repo.UpdateDynamicAgentProfile(ctx,
		&models.DynamicAgentProfile{ProfileID: profile.ID, KeepModelWhileRunning: false}, 1, nil); err != nil {
		t.Fatalf("clear routes: %v", err)
	}
	cleared, routes, err := repo.GetDynamicAgentProfile(ctx, profile.ID)
	if err != nil {
		t.Fatalf("GetDynamicAgentProfile after clear: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("routes = %d, want 0", len(routes))
	}
	if cleared.KeepModelWhileRunning {
		t.Fatalf("keep model = true, want the cleared preference to survive an empty list")
	}

	// Repopulating the list carries the same preference forward.
	if err := repo.UpdateDynamicAgentProfile(ctx,
		&models.DynamicAgentProfile{ProfileID: profile.ID, KeepModelWhileRunning: false}, 2,
		[]models.DynamicAgentRoute{
			{DynamicProfileID: profile.ID, Position: 0, ExecutionProfileID: "a", Enabled: true, RulesJSON: `{}`},
		}); err != nil {
		t.Fatalf("repopulate routes: %v", err)
	}
	repopulated, _, err := repo.GetDynamicAgentProfile(ctx, profile.ID)
	if err != nil {
		t.Fatalf("GetDynamicAgentProfile after repopulate: %v", err)
	}
	if repopulated.KeepModelWhileRunning {
		t.Fatalf("keep model = true, want the preference to survive repopulation")
	}
}

func TestDynamicProfileUpdateStillEnforcesOptimisticVersion(t *testing.T) {
	repo, _, ctx, profile := newDynamicSelectionRepo(t)
	if err := repo.CreateDynamicAgentProfile(ctx,
		&models.DynamicAgentProfile{ProfileID: profile.ID, KeepModelWhileRunning: true}, nil); err != nil {
		t.Fatalf("CreateDynamicAgentProfile: %v", err)
	}
	err := repo.UpdateDynamicAgentProfile(ctx,
		&models.DynamicAgentProfile{ProfileID: profile.ID, KeepModelWhileRunning: true}, 7, nil)
	if !errors.Is(err, ErrDynamicProfileVersionConflict) {
		t.Fatalf("error = %v, want %v", err, ErrDynamicProfileVersionConflict)
	}
	unchanged, _, err := repo.GetDynamicAgentProfile(ctx, profile.ID)
	if err != nil {
		t.Fatalf("GetDynamicAgentProfile: %v", err)
	}
	if unchanged.Version != 1 {
		t.Fatalf("version = %d, want the rejected update to leave the row at 1", unchanged.Version)
	}
}

// TestDynamicProfileKeepModelMigrationReplays covers both halves of the
// migration contract: a fresh database gets the inline column, and a legacy
// database that predates it is upgraded with the documented default of true.
func TestDynamicProfileKeepModelMigrationReplays(t *testing.T) {
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	legacy := `
		CREATE TABLE agents (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE agent_profiles (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			name TEXT NOT NULL,
			agent_display_name TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			user_modified INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);
		CREATE TABLE dynamic_agent_profiles (
			profile_id TEXT PRIMARY KEY,
			version INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);
	`
	if _, err := db.ExecContext(ctx, legacy); err != nil {
		t.Fatalf("build legacy schema: %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_profiles
			(id, agent_id, name, agent_display_name, enabled, user_modified, created_at, updated_at)
		VALUES ('legacy-dyn', 'legacy-agent', 'Legacy', 'Dynamic', 1, 0, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert legacy profile: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO dynamic_agent_profiles (profile_id, version, created_at, updated_at)
		VALUES ('legacy-dyn', 4, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert legacy dynamic profile: %v", err)
	}

	repo, err := newSQLiteRepository(db, db, nil, false)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	upgraded, routes, err := repo.GetDynamicAgentProfile(ctx, "legacy-dyn")
	if err != nil {
		t.Fatalf("GetDynamicAgentProfile after migration: %v", err)
	}
	if upgraded.Version != 4 {
		t.Fatalf("version = %d, want the pre-existing 4", upgraded.Version)
	}
	if !upgraded.KeepModelWhileRunning {
		t.Fatalf("keep model = false, want the migration default of true")
	}
	if len(routes) != 0 {
		t.Fatalf("routes = %d, want 0", len(routes))
	}

	// Replaying the same migration on the migrated database must stay a no-op.
	if _, err := newSQLiteRepository(db, db, nil, false); err != nil {
		t.Fatalf("replay migration: %v", err)
	}
}

func TestDynamicProfileKeepModelColumnIsNotNull(t *testing.T) {
	_, db, ctx, _ := newDynamicSelectionRepo(t)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO dynamic_agent_profiles (profile_id, version, created_at, updated_at)
		 VALUES ('no-flag', 1, ?, ?)`, time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatalf("insert without the flag: %v", err)
	}
	var keep int
	if err := db.GetContext(ctx, &keep,
		`SELECT keep_model_while_running FROM dynamic_agent_profiles WHERE profile_id = ?`,
		"no-flag"); err != nil {
		t.Fatalf("read default flag: %v", err)
	}
	if keep != 1 {
		t.Fatalf("keep_model_while_running = %d, want the column default of 1", keep)
	}
}

var _ = sql.ErrNoRows
