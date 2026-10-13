package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/task/models"
)

// The fork's full (effect_type, created_at, ...) index on every effect row is
// replaced by a partial index over unsettled terminal settlements: startup
// drops the legacy index idempotently and the pending scan seeks the partial one.
func TestDeliveryPendingSettlementIndexReplacesLegacyIndex(t *testing.T) {
	repo := newRetentionTestRepo(t, "SQLite")
	ctx := context.Background()
	if _, err := repo.db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_delivery_effects_pending
		ON agent_delivery_effects(effect_type, created_at, stream_id, sequence)`); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repo.initAgentDeliveryRetentionSchema(); err != nil {
			t.Fatalf("schema upgrade %d: %v", attempt+1, err)
		}
	}
	if legacy, err := db.IndexExistsContext(ctx, repo.db, agentDeliveryLegacyPendingIndexName); err != nil || legacy {
		t.Fatalf("legacy pending index still present: %v, %v", legacy, err)
	}
	if partial, err := db.IndexExistsContext(ctx, repo.db, agentDeliveryPendingSettlementIndexName); err != nil || !partial {
		t.Fatalf("partial pending-settlement index missing: %v, %v", partial, err)
	}
	if err := repo.CheckAgentDeliveryRetentionReady(ctx); err != nil {
		t.Fatalf("readiness after index replacement: %v", err)
	}

	base := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	for index, effect := range []struct {
		session string
		state   string
	}{{"pending-a", models.DeliveryEffectPending}, {"pending-b", models.DeliveryEffectCompleted}, {"pending-a", "block_resolved"}, {"pending-b", models.DeliveryEffectPending}} {
		at := base.Add(time.Duration(index) * time.Minute)
		row := &models.AgentDeliveryEffect{
			EffectKey: "agent_delivery.terminal:pending-stream:" + string(rune('1'+index)), StreamID: "pending-stream",
			Sequence: int64(index + 1), EffectType: models.DeliveryTerminalEffectType, State: effect.state,
			SessionID: effect.session, IncarnationID: "inc", HarnessGeneration: 1, CreatedAt: at,
		}
		if effect.state == models.DeliveryEffectCompleted {
			row.CompletedAt = &at
		}
		if _, err := repo.PutAgentDeliveryEffect(ctx, row); err != nil {
			t.Fatalf("seed effect %d: %v", index+1, err)
		}
	}
	all, err := repo.ListPendingAgentDeliverySettlements(ctx, "")
	if err != nil || len(all) != 3 || all[0].Sequence != 1 || all[1].Sequence != 3 || all[2].Sequence != 4 {
		t.Fatalf("pending settlements = %+v, %v; want sequences 1,3,4 in creation order", all, err)
	}
	scoped, err := repo.ListPendingAgentDeliverySettlements(ctx, "pending-a")
	if err != nil || len(scoped) != 2 {
		t.Fatalf("session-scoped pending settlements = %+v, %v", scoped, err)
	}

	for _, sessionID := range []string{"", "pending-a"} {
		rows, err := repo.db.Query("EXPLAIN QUERY PLAN "+agentDeliveryPendingSettlementsQuery, sessionID, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		_ = rows.Close()
		joined := strings.Join(plan, " | ")
		if !strings.Contains(joined, "USING INDEX "+agentDeliveryPendingSettlementIndexName) ||
			strings.Contains(joined, "TEMP B-TREE") || joined == "SCAN agent_delivery_effects" {
			t.Fatalf("pending settlement scan must read the partial index in order: %s", joined)
		}
		t.Logf("pending settlement plan (session %q): %s", sessionID, joined)
	}
}
