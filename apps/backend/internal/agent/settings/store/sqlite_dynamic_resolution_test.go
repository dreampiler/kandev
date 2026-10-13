package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kandev/kandev/internal/agent/settings/models"
)

func newResolutionStore(t *testing.T) (*sqliteRepository, context.Context, string) {
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
	if err := repo.CreateAgent(ctx, &models.Agent{ID: "family", Name: "family"}); err != nil {
		t.Fatalf("create family: %v", err)
	}
	target := &models.AgentProfile{ID: "target", AgentID: "family", Name: "Target", AgentDisplayName: "Family"}
	if err := repo.CreateAgentProfile(ctx, target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	return repo, ctx, target.ID
}

func seedReferencingDynamic(t *testing.T, repo *sqliteRepository, ctx context.Context, dynamicID, targetID string) {
	t.Helper()
	profile := &models.AgentProfile{ID: dynamicID, AgentID: "family", Name: dynamicID, AgentDisplayName: "Family"}
	if err := repo.CreateAgentProfile(ctx, profile); err != nil {
		t.Fatalf("create dynamic parent %s: %v", dynamicID, err)
	}
	doc := &models.DynamicAgentProfile{ProfileID: dynamicID, Version: 1}
	routes := []models.DynamicAgentRoute{
		{DynamicProfileID: dynamicID, Position: 0, ExecutionProfileID: targetID, Enabled: true, RulesJSON: `{}`},
	}
	if err := repo.CreateDynamicAgentProfile(ctx, doc, routes); err != nil {
		t.Fatalf("create dynamic document %s: %v", dynamicID, err)
	}
}

// TestResolveDynamicReferencesDeleteRollsBackWholeSet pins AC-.5: any version
// mismatch across the referencing set rolls back every dynamic profile and the
// target row, so no partial state is observable.
func TestResolveDynamicReferencesDeleteRollsBackWholeSet(t *testing.T) {
	repo, ctx, targetID := newResolutionStore(t)
	seedReferencingDynamic(t, repo, ctx, "d1", targetID)
	seedReferencingDynamic(t, repo, ctx, "d2", targetID)

	resolutions := []DynamicProfileResolution{
		{ProfileID: "d1", ExpectedVersion: 1, Routes: []models.DynamicAgentRoute{
			{DynamicProfileID: "d1", Position: 0, ExecutionProfileID: targetID, Enabled: false, RulesJSON: `{}`},
		}},
		{ProfileID: "d2", ExpectedVersion: 99, Routes: nil},
	}
	err := repo.ResolveDynamicReferencesAndDeleteProfile(ctx, targetID, resolutions)
	if !errors.Is(err, ErrDynamicProfileVersionConflict) {
		t.Fatalf("err = %v, want ErrDynamicProfileVersionConflict", err)
	}
	for _, id := range []string{"d1", "d2"} {
		doc, routes, err := repo.GetDynamicAgentProfile(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if doc.Version != 1 || len(routes) != 1 || !routes[0].Enabled {
			t.Fatalf("%s changed after rollback: version=%d routes=%#v", id, doc.Version, routes)
		}
	}
	if _, err := repo.GetAgentProfile(ctx, targetID); err != nil {
		t.Fatalf("target must survive the rolled-back delete: %v", err)
	}
}

// TestResolveDynamicReferencesDeleteAppliesAll writes every referencing
// document and soft-deletes the target in one call.
func TestResolveDynamicReferencesDeleteAppliesAll(t *testing.T) {
	repo, ctx, targetID := newResolutionStore(t)
	seedReferencingDynamic(t, repo, ctx, "d1", targetID)
	seedReferencingDynamic(t, repo, ctx, "d2", targetID)

	resolutions := []DynamicProfileResolution{
		{ProfileID: "d1", ExpectedVersion: 1, Routes: nil},
		{ProfileID: "d2", ExpectedVersion: 1, Routes: []models.DynamicAgentRoute{
			{DynamicProfileID: "d2", Position: 0, ExecutionProfileID: "someone-else", Enabled: true, RulesJSON: `{}`},
		}},
	}
	if err := repo.ResolveDynamicReferencesAndDeleteProfile(ctx, targetID, resolutions); err != nil {
		t.Fatalf("ResolveDynamicReferencesAndDeleteProfile: %v", err)
	}
	for _, id := range []string{"d1", "d2"} {
		doc, _, err := repo.GetDynamicAgentProfile(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if doc.Version != 2 {
			t.Fatalf("%s version = %d, want 2", id, doc.Version)
		}
	}
	if _, err := repo.GetAgentProfile(ctx, targetID); err == nil {
		t.Fatal("target profile must be soft-deleted")
	}
	deleted, err := repo.GetAgentProfileIncludingDeleted(ctx, targetID)
	if err != nil || deleted.DeletedAt == nil {
		t.Fatalf("target row = %#v err = %v, want soft-deleted", deleted, err)
	}
}

// TestResolveDynamicReferencesUpdateDisablesTarget covers the disable path:
// every referencing document is rewritten and the target row is disabled in one
// transaction.
func TestResolveDynamicReferencesUpdateDisablesTarget(t *testing.T) {
	repo, ctx, targetID := newResolutionStore(t)
	seedReferencingDynamic(t, repo, ctx, "d1", targetID)
	seedReferencingDynamic(t, repo, ctx, "d2", targetID)

	resolutions := []DynamicProfileResolution{
		{ProfileID: "d1", ExpectedVersion: 1, Routes: []models.DynamicAgentRoute{
			{DynamicProfileID: "d1", Position: 0, ExecutionProfileID: targetID, Enabled: false, RulesJSON: `{}`},
		}},
		{ProfileID: "d2", ExpectedVersion: 1, Routes: []models.DynamicAgentRoute{
			{DynamicProfileID: "d2", Position: 0, ExecutionProfileID: targetID, Enabled: false, RulesJSON: `{}`},
		}},
	}
	target, err := repo.GetAgentProfile(ctx, targetID)
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	disabled := false
	if err := repo.ResolveDynamicReferencesAndUpdateProfile(ctx, target, &disabled, resolutions); err != nil {
		t.Fatalf("ResolveDynamicReferencesAndUpdateProfile: %v", err)
	}
	if target.Enabled {
		t.Fatal("target profile must be disabled")
	}
	for _, id := range []string{"d1", "d2"} {
		doc, routes, err := repo.GetDynamicAgentProfile(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if doc.Version != 2 || len(routes) != 1 || routes[0].Enabled {
			t.Fatalf("%s = version %d routes %#v, want version 2 disabled row", id, doc.Version, routes)
		}
	}
	stored, err := repo.GetAgentProfile(ctx, targetID)
	if err != nil || stored.Enabled {
		t.Fatalf("stored target = %#v err = %v, want disabled", stored, err)
	}
}
