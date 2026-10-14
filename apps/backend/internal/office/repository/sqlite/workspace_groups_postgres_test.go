package sqlite_test

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/office/models"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
	taskrepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	"github.com/kandev/kandev/internal/testutil"
)

func TestPostgresWorkspaceGroupOwnershipRoundTrip(t *testing.T) {
	db := testutil.OpenIsolatedPostgres(t, testutil.PostgresDSNFromEnv(t))
	if _, err := taskrepo.NewWithDB(db, db, nil); err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.NewWithDB(db, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, owned := range []bool{false, true} {
		group := &models.WorkspaceGroup{WorkspaceID: "ownership-workspace", OwnerTaskID: "ownership-task",
			MaterializedKind: models.WorkspaceGroupKindPlainFolder, OwnedByKandev: owned}
		if err := repo.CreateWorkspaceGroup(ctx, group); err != nil {
			t.Fatal(err)
		}
		assertPostgresWorkspaceGroupOwned(t, repo, group.ID, owned)
		for _, materializedOwned := range []bool{true, false} {
			if err := repo.MarkWorkspaceMaterialized(ctx, group.ID, models.MaterializedWorkspace{
				Kind: models.WorkspaceGroupKindPlainFolder, Path: "/recorded/workspace", OwnedByKandev: materializedOwned,
			}); err != nil {
				t.Fatal(err)
			}
			assertPostgresWorkspaceGroupOwned(t, repo, group.ID, materializedOwned)
		}
	}
}

func assertPostgresWorkspaceGroupOwned(t *testing.T, repo *sqlite.Repository, id string, owned bool) {
	t.Helper()
	group, err := repo.GetWorkspaceGroup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if group == nil || group.OwnedByKandev != owned {
		t.Fatalf("workspace ownership did not round trip: group=%+v want owned=%v", group, owned)
	}
}
