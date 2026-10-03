package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/testutil"
)

// TestPostgresListAnswerableClarificationsForSessions runs the
// session-bounded read on PostgreSQL, where the JSON expressions and the
// aggregate created_at scan take their PostgreSQL branches.
func TestPostgresListAnswerableClarificationsForSessions(t *testing.T) {
	db := testutil.OpenIsolatedPostgres(t, testutil.PostgresDSNFromEnv(t))
	repo, err := NewWithDB(db, db, nil)
	if err != nil {
		t.Fatalf("init postgres schema: %v", err)
	}
	ctx := context.Background()

	seedBundleTask(t, repo, "task-pg-ov", "")
	seedBundleSession(t, repo, "sess-pg-ov", "task-pg-ov")
	seedBundleTurn(t, repo, "turn-pg-ov", "sess-pg-ov", "task-pg-ov")
	ts := time.Now().UTC().Truncate(time.Millisecond)
	insertClarificationMessage(t, repo, "msg-pg-ov", "sess-pg-ov", "task-pg-ov", "turn-pg-ov", "pending-pg-ov", "q1", "pending", 0, ts)

	bundles, err := repo.ListAnswerableClarificationsForSessions(ctx, []string{"sess-pg-ov", "sess-pg-missing"})
	if err != nil {
		t.Fatalf("ListAnswerableClarificationsForSessions: %v", err)
	}
	if len(bundles) != 1 || bundles[0].PendingID != "pending-pg-ov" || !bundles[0].CreatedAt.Equal(ts) {
		t.Fatalf("bundles = %+v, want pending-pg-ov created at %v", bundles, ts)
	}
}
