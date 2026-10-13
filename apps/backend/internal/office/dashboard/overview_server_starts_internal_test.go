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

	got := serverStartEvents(starts, "fallback", since, time.Time{})
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

	got := serverStartEvents(starts, "fallback", since, time.Time{})
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 (out-of-window dropped)", len(got))
	}
	if got[0].Version != "fallback" {
		t.Fatalf("version = %q, want the running build fallback", got[0].Version)
	}
}

// TestServerStartEvents_FallsBackToRunningProcess proves an empty durable
// history still shows the running process, so a boot whose record write failed
// (or a fresh install, or an upgrade) is not reported as zero startups.
func TestServerStartEvents_FallsBackToRunningProcess(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	boot := now.Add(-time.Minute)

	got := serverStartEvents(nil, "v9", since, boot)
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 (running process)", len(got))
	}
	if !got[0].At.Equal(boot) || got[0].Version != "v9" {
		t.Fatalf("row = %+v, want the running boot at %s with version v9", got[0], boot)
	}
}

// TestServerStartEvents_NoFallbackWhenHistoryPresent proves the running
// process is not added on top of a durable record, so a normal boot is counted
// once rather than twice.
func TestServerStartEvents_NoFallbackWhenHistoryPresent(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	starts := []*sqlite.OverviewServerStartRow{
		serverStartRecord(now.Add(-time.Hour), "v9"),
	}

	got := serverStartEvents(starts, "v9", since, now.Add(-time.Minute))
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 (record only, no running-process fallback)", len(got))
	}
}

// TestServerStartEvents_NoFallbackOutsideWindow proves a running process that
// started before the window is not shown, so a server up for longer than the
// window reports no startups rather than one stale entry.
func TestServerStartEvents_NoFallbackOutsideWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	got := serverStartEvents(nil, "v9", since, now.Add(-48*time.Hour))
	if len(got) != 0 {
		t.Fatalf("rows = %d, want 0 (running process outside window)", len(got))
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

	got := assembleEvents(snap, starts, nil, nil, nil, since, time.Time{})
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

	if empty := assembleEvents(snap, nil, nil, nil, nil, since, time.Time{}); len(empty) != 0 {
		t.Fatalf("empty window rows = %d, want 0", len(empty))
	}
}

// TestAssembleEvents_RunningProcessFallback proves the assembled list shows the
// running process when the durable history is empty, so the overview is never
// empty while a server is up.
func TestAssembleEvents_RunningProcessFallback(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	snap := &overviewSnapshot{buildVersion: "v9"}

	got := assembleEvents(snap, nil, nil, nil, nil, since, now.Add(-time.Minute))
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 (running process)", len(got))
	}
	if got[0].Kind != overviewEventServerStarted || got[0].Version != "v9" {
		t.Fatalf("row = %+v, want one server-started row with version v9", got[0])
	}
}
