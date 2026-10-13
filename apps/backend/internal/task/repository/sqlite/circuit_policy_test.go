package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

func TestCircuitPolicyPersistsFailureTimeInUTC(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	local := time.Date(2026, 10, 7, 13, 47, 0, 0, time.FixedZone("KST", 9*60*60))
	if err := repo.SaveCircuit(ctx, dynamic.CircuitSnapshot{
		Key: "model:timezone", State: dynamic.CircuitOpen,
		Code: routingerr.CodeRateLimited, UpdatedAt: local,
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := repo.db.QueryRowContext(ctx, `SELECT CAST(updated_at AS TEXT) FROM dynamic_resource_circuits WHERE resource_key = ?`, "model:timezone").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "2026-10-07 04:47:00") || !strings.HasSuffix(raw, "+00:00") {
		t.Fatalf("stored failure time = %q, want the same instant in UTC", raw)
	}
}

func TestCircuitPolicyRestorePreservesFailureTimeAndDurableCap(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	updated := time.Date(2026, 10, 7, 4, 47, 0, 0, time.UTC)
	key := "model:credential:profile:go|opencode-go/glm-5.3-flash"
	if err := repo.SaveCircuit(ctx, dynamic.CircuitSnapshot{Key: key, State: dynamic.CircuitOpen, Code: routingerr.CodeQuotaLimited, Until: updated.Add(23 * 24 * time.Hour), UpdatedAt: updated, Strikes: 2}); err != nil {
		t.Fatal(err)
	}
	registry := dynamic.NewCircuitRegistry(dynamic.WithCircuitPersistence(repo))
	if err := registry.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	want := updated.Add(7 * 24 * time.Hour)
	if got := registry.Inspect(key, updated).Until; !got.Equal(want) {
		t.Fatalf("restored expiry = %v, want %v", got, want)
	}
	snapshots, err := repo.LoadCircuits(ctx)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("load = %v, %v", snapshots, err)
	}
	if !snapshots[0].UpdatedAt.Equal(updated) || !snapshots[0].Until.Equal(want) {
		t.Fatalf("saved cap moved original failure time: %#v", snapshots[0])
	}
	if released, err := registry.Release(key); !released || err != nil {
		t.Fatalf("release = %v, %v", released, err)
	}
	restarted := dynamic.NewCircuitRegistry(dynamic.WithCircuitPersistence(repo))
	if err := restarted.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if restarted.Inspect(key, updated).State != dynamic.ResourceAvailable {
		t.Fatal("released circuit resurrected")
	}
}
