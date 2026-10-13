package persistence

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jmoiron/sqlx"
)

// serverStartsKey is the kandev_meta key holding the install-wide history of
// backend boot instants. It is one bounded JSON array rather than a table: the
// list exists only to make the Office overview's startup count durable across a
// process replacement, and it stays small enough (one entry per boot) that the
// overview's fixed 24-hour window needs no indexed query.
const serverStartsKey = "server_started_events"

// serverStartsRetention and serverStartsMaxEntries bound that list. Boots are
// rare (a handful a day at most), so both limits are far above realistic use and
// exist only so the value can never grow without bound.
const (
	serverStartsRetention  = 31 * 24 * time.Hour
	serverStartsMaxEntries = 200
)

// ServerStart is one recorded backend boot: the instant the server became
// startable and the binary version that booted. The instant is UTC.
type ServerStart struct {
	At      time.Time `json:"at"`
	Version string    `json:"version"`
}

// RecordServerStarted appends this boot's instant to the durable startup
// history. It is called once per boot, after the predecessor process has exited
// and while ownership is held, so the read-modify-write cannot race another
// writer. Old entries are pruned by age and count on every write.
func RecordServerStarted(db *sqlx.DB, at time.Time, version string) error {
	entries, err := readServerStarts(db)
	if err != nil {
		return err
	}
	entries = append(entries, ServerStart{At: at.UTC(), Version: version})
	entries = pruneServerStarts(entries, at.UTC())
	encoded, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode server starts: %w", err)
	}
	return writeKey(db, serverStartsKey, string(encoded))
}

// ReadServerStartsSince returns the recorded boots whose instant is at or after
// since, newest first. An absent or unreadable value yields no entries rather
// than an error: the list is a display history, and a corrupt value must not
// take the whole overview down.
func ReadServerStartsSince(db *sqlx.DB, since time.Time) ([]ServerStart, error) {
	entries, err := readServerStarts(db)
	if err != nil {
		return nil, err
	}
	out := make([]ServerStart, 0, len(entries))
	for _, entry := range entries {
		if entry.At.Before(since) {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// readServerStarts decodes the stored array. A malformed value is treated as no
// history, matching the read's tolerant contract.
func readServerStarts(db *sqlx.DB) ([]ServerStart, error) {
	raw, err := readKey(db, serverStartsKey)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var entries []ServerStart
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, nil
	}
	return entries, nil
}

// pruneServerStarts drops entries older than the retention window and keeps
// only the newest serverStartsMaxEntries. It sorts by instant first so the
// retention and the cap hold even if a skewed clock wrote an entry out of
// order.
func pruneServerStarts(entries []ServerStart, now time.Time) []ServerStart {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.Before(entries[j].At) })
	cutoff := now.Add(-serverStartsRetention)
	out := make([]ServerStart, 0, len(entries))
	for _, entry := range entries {
		if entry.At.Before(cutoff) {
			continue
		}
		out = append(out, entry)
	}
	if len(out) > serverStartsMaxEntries {
		out = out[len(out)-serverStartsMaxEntries:]
	}
	return out
}
