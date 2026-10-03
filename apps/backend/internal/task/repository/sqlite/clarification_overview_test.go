package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// TestListAnswerableClarificationsForSessions_BoundsToSessions pins that the
// session-bounded read returns the answerable bundles of the named sessions
// only, and applies the same answerable rules as the inbox list (an answered
// bundle and a bundle on a terminal session are left out).
func TestListAnswerableClarificationsForSessions_BoundsToSessions(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	now := time.Now().UTC()

	seedBundleTask(t, repo, "task-ov-1", "")
	seedBundleSession(t, repo, "sess-ov-1", "task-ov-1")
	seedBundleTurn(t, repo, "turn-ov-1", "sess-ov-1", "task-ov-1")
	insertClarificationMessage(t, repo, "msg-ov-1", "sess-ov-1", "task-ov-1", "turn-ov-1", "pending-ov-1", "q1", "pending", 0, now)

	seedBundleTask(t, repo, "task-ov-2", "")
	seedBundleSession(t, repo, "sess-ov-2", "task-ov-2")
	seedBundleTurn(t, repo, "turn-ov-2", "sess-ov-2", "task-ov-2")
	insertClarificationMessage(t, repo, "msg-ov-2", "sess-ov-2", "task-ov-2", "turn-ov-2", "pending-ov-2", "q1", "pending", 0, now)

	seedBundleTask(t, repo, "task-ov-3", "")
	seedBundleSessionWithState(t, repo, "sess-ov-3", "task-ov-3", models.TaskSessionStateCompleted)
	seedBundleTurn(t, repo, "turn-ov-3", "sess-ov-3", "task-ov-3")
	insertClarificationMessage(t, repo, "msg-ov-3", "sess-ov-3", "task-ov-3", "turn-ov-3", "pending-ov-3", "q1", "pending", 0, now)

	seedBundleTask(t, repo, "task-ov-4", "")
	seedBundleSession(t, repo, "sess-ov-4", "task-ov-4")
	seedBundleTurn(t, repo, "turn-ov-4", "sess-ov-4", "task-ov-4")
	insertClarificationMessage(t, repo, "msg-ov-4", "sess-ov-4", "task-ov-4", "turn-ov-4", "pending-ov-4", "q1", "answered", 0, now)

	bundles, err := repo.ListAnswerableClarificationsForSessions(ctx, []string{"sess-ov-1", "sess-ov-3", "sess-ov-4"})
	if err != nil {
		t.Fatalf("ListAnswerableClarificationsForSessions: %v", err)
	}
	if len(bundles) != 1 || bundles[0].PendingID != "pending-ov-1" || bundles[0].TaskID != "task-ov-1" {
		t.Fatalf("bundles = %+v, want exactly pending-ov-1 on task-ov-1", bundles)
	}

	empty, err := repo.ListAnswerableClarificationsForSessions(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("no sessions: bundles = %+v, err = %v; want none", empty, err)
	}

	// The inbox list is unchanged by the shared expression's new inner filter.
	page, err := repo.ListUnresolvedClarificationBundles(ctx, unscopedOpts(50))
	if err != nil {
		t.Fatalf("ListUnresolvedClarificationBundles: %v", err)
	}
	if len(page.Bundles) != 2 {
		t.Fatalf("inbox bundles = %+v, want pending-ov-1 and pending-ov-2", page.Bundles)
	}
}
