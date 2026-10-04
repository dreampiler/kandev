package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// The workspace-scoped inbox read bounds its grouped message scan to the
// workspace's own sessions, so a bundle whose message rows carry a legacy empty
// task_id must still resolve through MIN(ts.task_id) and stay listed — the scan
// narrowing must not turn into a result narrowing.
func withSidecar(opts models.ListClarificationBundlesOptions) models.ListClarificationBundlesOptions {
	opts.Sidecar = &models.ClarificationSidecarFilter{UserID: "user-1", Now: time.Now().UTC()}
	return opts
}

func TestListUnresolvedClarificationBundles_WorkspaceScanKeepsSessionResolvedTaskID(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	for _, id := range []string{"ws-scan", "ws-other-scan"} {
		if err := repo.CreateWorkspace(ctx, &models.Workspace{ID: id, Name: id}); err != nil {
			t.Fatalf("create workspace %s: %v", id, err)
		}
	}

	seedBundleTask(t, repo, "task-scan", "ws-scan")
	seedBundleSession(t, repo, "sess-scan", "task-scan")
	seedBundleTurn(t, repo, "turn-scan", "sess-scan", "task-scan")
	insertClarificationMessage(t, repo, "msg-scan", "sess-scan", "", "turn-scan", "pending-scan", "q1", "pending", 0, time.Now().UTC())

	seedBundleTask(t, repo, "task-other-scan", "ws-other-scan")
	seedBundleSession(t, repo, "sess-other-scan", "task-other-scan")
	seedBundleTurn(t, repo, "turn-other-scan", "sess-other-scan", "task-other-scan")
	insertClarificationMessage(t, repo, "msg-other-scan", "sess-other-scan", "task-other-scan", "turn-other-scan",
		"pending-other-scan", "q1", "pending", 0, time.Now().UTC())

	opts := unscopedOpts(50)
	opts.WorkspaceID = "ws-scan"
	page, err := repo.ListUnresolvedClarificationBundles(ctx, opts)
	if err != nil {
		t.Fatalf("ListUnresolvedClarificationBundles: %v", err)
	}
	if len(page.Bundles) != 1 || page.Bundles[0].PendingID != "pending-scan" || page.Bundles[0].TaskID != "task-scan" {
		t.Fatalf("bundles = %+v, want only pending-scan resolved to task-scan", page.Bundles)
	}

	hidden, err := repo.CountHiddenClarificationBundles(ctx, withSidecar(opts))
	if err != nil {
		t.Fatalf("CountHiddenClarificationBundles: %v", err)
	}
	if hidden.HiddenCount != 1 {
		t.Fatalf("counted bundles = %d, want 1 (the page query and the count query must agree on the same workspace)", hidden.HiddenCount)
	}
}
