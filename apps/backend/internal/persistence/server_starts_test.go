package persistence

import (
	"testing"
	"time"
)

// TestRecordServerStarted_AccumulatesAcrossBoots proves the startup history is
// durable: two recorded boots both survive, newest first, so a backend
// replacement cannot reset the count to the running process alone.
func TestRecordServerStarted_AccumulatesAcrossBoots(t *testing.T) {
	db := memSQLiteDB(t)
	if err := ensureMetaTable(db); err != nil {
		t.Fatalf("ensureMetaTable: %v", err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	if err := RecordServerStarted(db, now.Add(-3*time.Hour), "v0.97.0-524-g7fd7cc4bae"); err != nil {
		t.Fatalf("record first boot: %v", err)
	}
	if err := RecordServerStarted(db, now, "v0.97.0-534-g3640339d52"); err != nil {
		t.Fatalf("record second boot: %v", err)
	}

	got, err := ReadServerStartsSince(db, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("boots in window = %d, want 2", len(got))
	}
	if !got[0].At.Equal(now) || got[0].Version != "v0.97.0-534-g3640339d52" {
		t.Fatalf("first row = %+v, want the newest boot", got[0])
	}
	if !got[1].At.Equal(now.Add(-3 * time.Hour)) {
		t.Fatalf("second row = %+v, want the older boot", got[1])
	}
}

// TestReadServerStartsSince_FiltersWindow verifies the window is applied on
// read: a boot before `since` is excluded and one after is kept.
func TestReadServerStartsSince_FiltersWindow(t *testing.T) {
	db := memSQLiteDB(t)
	if err := ensureMetaTable(db); err != nil {
		t.Fatalf("ensureMetaTable: %v", err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	if err := RecordServerStarted(db, now.Add(-48*time.Hour), "old"); err != nil {
		t.Fatalf("record old: %v", err)
	}
	if err := RecordServerStarted(db, now.Add(-time.Hour), "recent"); err != nil {
		t.Fatalf("record recent: %v", err)
	}

	got, err := ReadServerStartsSince(db, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].Version != "recent" {
		t.Fatalf("windowed read = %+v, want only the recent boot", got)
	}
}

// TestReadServerStartsSince_ToleratesMalformedValue verifies a corrupt stored
// value degrades to no history rather than failing the overview.
func TestReadServerStartsSince_ToleratesMalformedValue(t *testing.T) {
	db := memSQLiteDB(t)
	if err := ensureMetaTable(db); err != nil {
		t.Fatalf("ensureMetaTable: %v", err)
	}
	if err := writeKey(db, serverStartsKey, "{not json"); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	got, err := ReadServerStartsSince(db, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("read malformed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("malformed value = %+v, want no history", got)
	}
}

// TestPruneServerStarts_DropsOldAndCaps verifies the write-time bound: entries
// older than the retention are dropped and only the newest max entries remain.
func TestPruneServerStarts_DropsOldAndCaps(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	entries := []ServerStart{{At: now.Add(-serverStartsRetention - time.Hour), Version: "too-old"}}
	for i := 0; i < serverStartsMaxEntries+5; i++ {
		entries = append(entries, ServerStart{At: now.Add(-time.Duration(i) * time.Minute), Version: "recent"})
	}

	got := pruneServerStarts(entries, now)
	if len(got) != serverStartsMaxEntries {
		t.Fatalf("pruned length = %d, want %d", len(got), serverStartsMaxEntries)
	}
	for _, entry := range got {
		if entry.Version == "too-old" {
			t.Fatal("an entry older than the retention must be pruned")
		}
	}
}
