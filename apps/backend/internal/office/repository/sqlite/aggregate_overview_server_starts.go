package sqlite

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/persistence"
)

// OverviewServerStartRow is one recorded backend boot inside the overview's
// window: when the server became startable and the binary version that booted.
type OverviewServerStartRow struct {
	At      time.Time
	Version string
}

// ListOverviewServerStarts returns the backend boots recorded since `since`,
// newest first, up to limit. The history lives in kandev_meta (written once per
// boot), not in a process-local value, so the last-24-hours list reports every
// startup in the window instead of only the running process. The read is
// read-only; the record is written at boot. A limit of zero or less means no
// cap.
func (r *Repository) ListOverviewServerStarts(
	_ context.Context, since time.Time, limit int,
) ([]*OverviewServerStartRow, error) {
	entries, err := persistence.ReadServerStartsSince(r.ro, since)
	if err != nil {
		return nil, err
	}
	out := make([]*OverviewServerStartRow, 0, len(entries))
	for _, entry := range entries {
		out = append(out, &OverviewServerStartRow{At: entry.At, Version: entry.Version})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
