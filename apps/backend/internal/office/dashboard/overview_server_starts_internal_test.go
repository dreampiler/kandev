package dashboard

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

func serverStartRecord(at time.Time, version string) *sqlite.OverviewServerStartRow {
	return &sqlite.OverviewServerStartRow{At: at, Version: version}
}

// TestServerStartEvents_OneRowPerRecord proves the kind renders one row per
// durable boot in the window, not one row for the running process, so the list
// and its chip count every startup after a replacement.
func TestServerStartEvents_OneRowPerRecord(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	starts := []*sqlite.OverviewServerStartRow{
		serverStartRecord(now.Add(-time.Minute), "v2"),
		serverStartRecord(now.Add(-3*time.Hour), "v1"),
	}

	got := serverStartEvents(starts, "fallback", since)
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	for _, event := range got {
		if event.Kind != overviewEventServerStarted {
			t.Fatalf("kind = %q, want %q", event.Kind, overviewEventServerStarted)
		}
	}
}

// TestServerStartEvents_FiltersWindowAndFallsBackVersion verifies an
// out-of-window record is dropped and a record without a version uses the
// running binary's version rather than rendering blank.
func TestServerStartEvents_FiltersWindowAndFallsBackVersion(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	starts := []*sqlite.OverviewServerStartRow{
		serverStartRecord(now.Add(-48*time.Hour), "too-old"),
		serverStartRecord(now.Add(-time.Hour), ""),
	}

	got := serverStartEvents(starts, "fallback", since)
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 (out-of-window dropped)", len(got))
	}
	if got[0].Version != "fallback" {
		t.Fatalf("version = %q, want the running build fallback", got[0].Version)
	}
}

// TestAssembleEvents_RendersEveryServerStart proves the assembled list carries
// one server-started row per record and none when the window is empty, and that
// the merged list is newest first.
func TestAssembleEvents_RendersEveryServerStart(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	snap := &overviewSnapshot{buildVersion: "v9"}
	starts := []*sqlite.OverviewServerStartRow{
		serverStartRecord(now.Add(-time.Minute), "v2"),
		serverStartRecord(now.Add(-2*time.Hour), "v1"),
		serverStartRecord(now.Add(-5*time.Hour), "v0"),
	}

	got := assembleEvents(snap, starts, nil, nil, nil, since)
	serverRows := 0
	var times []time.Time
	for _, event := range got {
		if event.Kind == overviewEventServerStarted {
			serverRows++
			times = append(times, event.At)
		}
	}
	if serverRows != 3 {
		t.Fatalf("server-started rows = %d, want 3", serverRows)
	}
	for i := 1; i < len(times); i++ {
		if times[i].After(times[i-1]) {
			t.Fatalf("server-started rows out of order: %v", times)
		}
	}

	if empty := assembleEvents(snap, nil, nil, nil, nil, since); len(empty) != 0 {
		t.Fatalf("empty window rows = %d, want 0", len(empty))
	}
}
