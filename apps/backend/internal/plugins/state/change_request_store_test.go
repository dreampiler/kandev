package state

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func newChangeRequestTestStore(t *testing.T) *ChangeRequestStore {
	t.Helper()
	raw, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	store := &ChangeRequestStore{db: raw, ro: raw}
	if err := store.initSchema(); err != nil {
		t.Fatalf("init change request schema: %v", err)
	}
	return store
}

func TestChangeRequestStoreReportAndLatch(t *testing.T) {
	store := newChangeRequestTestStore(t)
	ctx := context.Background()
	mergedAt := time.Now().UTC().Add(-time.Hour).Format(changeRequestTimestampLayout)
	first, existed, err := store.ReportChangeRequest(ctx, ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", ProviderHost: "forge.example.test", RepositoryID: "repo-1",
		Number: 7, URL: "https://forge.example.test/owner/repo/pulls/7", Title: "Fix it",
		State: ChangeRequestStateMerged, HeadBranch: "fix", BaseBranch: "main",
		MergedAt: mergedAt,
	})
	if err != nil {
		t.Fatalf("report merged: %v", err)
	}
	if existed {
		t.Fatal("first report must be new")
	}
	if first.MergedAt != mergedAt {
		t.Fatalf("merged_at = %q, want %q", first.MergedAt, mergedAt)
	}
	// A stale re-report without merged_at must not clear the latch.
	second, existed, err := store.ReportChangeRequest(ctx, ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 7,
		Title: "Fix it (updated)", State: ChangeRequestStateMerged,
	})
	if err != nil {
		t.Fatalf("re-report: %v", err)
	}
	if !existed {
		t.Fatal("second report must be an update")
	}
	if second.MergedAt != mergedAt {
		t.Fatalf("merged_at after stale re-report = %q, want latched %q", second.MergedAt, mergedAt)
	}
	if second.Title != "Fix it (updated)" {
		t.Fatalf("title = %q, want latest report", second.Title)
	}
	if second.ID != first.ID {
		t.Fatal("stable id must survive re-reports")
	}
	// A stale report that says the request is open must not unmerge it.
	third, existed, err := store.ReportChangeRequest(ctx, ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 7,
		State: ChangeRequestStateOpen,
	})
	if err != nil {
		t.Fatalf("stale unmerge report: %v", err)
	}
	if !existed || third.State != ChangeRequestStateMerged || third.MergedAt != mergedAt {
		t.Fatalf("stale unmerge report = %+v, want merged with latched merged_at", third)
	}
}

func TestChangeRequestStoreRejectsBadState(t *testing.T) {
	store := newChangeRequestTestStore(t)
	_, _, err := store.ReportChangeRequest(context.Background(), ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 1, State: "squashed",
	})
	if err == nil {
		t.Fatal("unknown state must be rejected")
	}
}

func TestChangeRequestStoreRemove(t *testing.T) {
	store := newChangeRequestTestStore(t)
	ctx := context.Background()
	mergedAt := time.Now().UTC().Add(-30 * time.Minute).Format(changeRequestTimestampLayout)
	if _, _, err := store.ReportChangeRequest(ctx, ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 3,
		State: ChangeRequestStateMerged, MergedAt: mergedAt,
	}); err != nil {
		t.Fatalf("report merged: %v", err)
	}
	if err := store.RemoveChangeRequest(ctx, "inst-1", "ws-1", "forgejo", "repo-1", 3); err != nil {
		t.Fatalf("remove: %v", err)
	}
	remaining, err := store.ListChangeRequestsByTask(ctx, "inst-1", "ws-1", "task-1")
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("rows after remove = %d, want 0", len(remaining))
	}
	// Removing a missing row is not an error.
	if err := store.RemoveChangeRequest(ctx, "inst-1", "ws-1", "forgejo", "repo-1", 3); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestChangeRequestStoreRejectsInvalidTimestamp(t *testing.T) {
	store := newChangeRequestTestStore(t)
	_, _, err := store.ReportChangeRequest(context.Background(), ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 1,
		State: ChangeRequestStateOpen, CreatedAt: "not-a-timestamp",
	})
	if err == nil {
		t.Fatal("invalid timestamp must be rejected")
	}
	_, _, err = store.ReportChangeRequest(context.Background(), ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 2,
		State: ChangeRequestStateMerged,
	})
	if err == nil {
		t.Fatal("new merged row without merged_at must be rejected")
	}
}

func TestChangeRequestStoreDeleteByInstallation(t *testing.T) {
	store := newChangeRequestTestStore(t)
	ctx := context.Background()
	for _, inst := range []string{"inst-1", "inst-2"} {
		if _, _, err := store.ReportChangeRequest(ctx, ChangeRequestRecord{
			InstallationID: inst, WorkspaceID: "ws-1", TaskID: "task-1",
			ProviderID: "forgejo", RepositoryID: "repo-1", Number: 1, State: ChangeRequestStateOpen,
		}); err != nil {
			t.Fatalf("report %s: %v", inst, err)
		}
	}
	if err := store.DeleteByInstallation(ctx, "inst-1"); err != nil {
		t.Fatalf("delete by installation: %v", err)
	}
	remaining, err := store.ListChangeRequestsByTask(ctx, "inst-2", "ws-1", "task-1")
	if err != nil {
		t.Fatalf("list remaining: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining rows = %d, want 1", len(remaining))
	}
	removed, err := store.ListChangeRequestsByTask(ctx, "inst-1", "ws-1", "task-1")
	if err != nil {
		t.Fatalf("list removed: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed rows = %d, want 0", len(removed))
	}
}

func TestChangeRequestStoreClosedStateIsTerminal(t *testing.T) {
	store := newChangeRequestTestStore(t)
	ctx := context.Background()
	closedAt := time.Now().UTC().Add(-time.Minute).Format(changeRequestTimestampLayout)
	if _, _, err := store.ReportChangeRequest(ctx, ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 8,
		State: ChangeRequestStateClosed, ClosedAt: closedAt,
	}); err != nil {
		t.Fatalf("report closed: %v", err)
	}
	reopened, existed, err := store.ReportChangeRequest(ctx, ChangeRequestRecord{
		InstallationID: "inst-1", WorkspaceID: "ws-1", TaskID: "task-1",
		ProviderID: "forgejo", RepositoryID: "repo-1", Number: 8,
		State: ChangeRequestStateOpen,
	})
	if err != nil {
		t.Fatalf("stale reopen report: %v", err)
	}
	if !existed || reopened.State != ChangeRequestStateClosed || reopened.ClosedAt != closedAt {
		t.Fatalf("stale reopen report = %+v, want closed with latched closed_at", reopened)
	}
}
