package backendapp

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
)

type fakeCoordinatorProfileStore struct {
	settingsstore.Repository
	profiles map[string]*settingsmodels.AgentProfile
	err      error
}

func (f fakeCoordinatorProfileStore) GetAgentProfile(_ context.Context, id string) (*settingsmodels.AgentProfile, error) {
	if f.err != nil {
		return nil, f.err
	}
	if profile, ok := f.profiles[id]; ok {
		return profile, nil
	}
	return nil, sql.ErrNoRows
}

func TestCeilingCoordinatorProfilesReadsTheDurableRole(t *testing.T) {
	deleted := time.Now()
	store := fakeCoordinatorProfileStore{profiles: map[string]*settingsmodels.AgentProfile{
		"ceo":         {ID: "ceo", Role: settingsmodels.AgentRoleCEO, WorkspaceID: "ws-1"},
		"worker":      {ID: "worker", Role: settingsmodels.AgentRoleWorker, WorkspaceID: "ws-1"},
		"global-ceo":  {ID: "global-ceo", Role: settingsmodels.AgentRoleCEO},
		"deleted-ceo": {ID: "deleted-ceo", Role: settingsmodels.AgentRoleCEO, WorkspaceID: "ws-1", DeletedAt: &deleted},
	}}
	reader := ceilingCoordinatorProfiles{store: store}
	for id, want := range map[string]bool{
		"ceo": true, "worker": false, "global-ceo": false, "deleted-ceo": false, "missing": false,
	} {
		got, err := reader.IsWorkspaceCoordinatorProfile(context.Background(), id)
		if err != nil || got != want {
			t.Errorf("IsWorkspaceCoordinatorProfile(%q) = %v, %v; want %v", id, got, err, want)
		}
	}

	failing := ceilingCoordinatorProfiles{store: fakeCoordinatorProfileStore{err: errors.New("store down")}}
	if got, err := failing.IsWorkspaceCoordinatorProfile(context.Background(), "ceo"); err == nil || got {
		t.Fatalf("a store error = %v, %v; want false with the error", got, err)
	}
}
