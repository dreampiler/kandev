package db

import (
	"encoding/json"
	"expvar"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

// TestRegisterWriterPoolStatsExposesDBStatsAtDebugVars covers Defect 3's
// diagnostic half: nothing today exposes the writer pool's contention
// counters, so a WaitCount/WaitDuration spike (the actual mechanism behind
// the claim's `context deadline exceeded`, as opposed to a SQLite
// busy_timeout "database is locked") was invisible. RegisterWriterPoolStats
// must publish a JSON-decodable sql.DBStats snapshot under the documented
// expvar name.
func TestRegisterWriterPoolStatsExposesDBStatsAtDebugVars(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "writer-pool-stats.db")
	conn, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	writer := sqlx.NewDb(conn, "sqlite3")
	pool := NewPool(writer, writer)

	RegisterWriterPoolStats(pool)

	v := expvar.Get("db_writer_pool_stats")
	if v == nil {
		t.Fatal("db_writer_pool_stats not published")
	}
	var stats struct {
		MaxOpenConnections int
		OpenConnections    int
		InUse              int
		Idle               int
		WaitCount          int64
		WaitDuration       int64
	}
	if err := json.Unmarshal([]byte(v.String()), &stats); err != nil {
		t.Fatalf("unmarshal db_writer_pool_stats: %v (raw: %s)", err, v.String())
	}
	if stats.MaxOpenConnections != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1 (single-writer pool)", stats.MaxOpenConnections)
	}

	// Guards the sync.Once: a second call (e.g. a future second devMode
	// wiring path) must not panic with expvar's "Reuse of exported var name"
	// error. This has to be the same test as the first call above, not a
	// separate one: expvar.Publish has no unpublish, so whichever test in
	// this process registers "db_writer_pool_stats" first permanently
	// consumes the package's sync.Once, and a same-named second test can
	// only ever see it already consumed -- it would pass whether or not the
	// guard actually works.
	RegisterWriterPoolStats(pool)
}

// TestSQLiteReaderConnsResolvesEnvironmentOverride pins the read pool sizing:
// the default applies unless the override is a positive integer, and the
// override is capped.
func TestSQLiteReaderConnsResolvesEnvironmentOverride(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want int
	}{
		{name: "unset", raw: "", want: defaultSQLiteReaderConns},
		{name: "override", raw: "20", want: 20},
		{name: "padded override", raw: " 8 ", want: 8},
		{name: "minimum", raw: "1", want: 1},
		{name: "not a number", raw: "many", want: defaultSQLiteReaderConns},
		{name: "zero", raw: "0", want: defaultSQLiteReaderConns},
		{name: "negative", raw: "-3", want: defaultSQLiteReaderConns},
		{name: "above cap", raw: "500", want: maxSQLiteReaderConns},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(sqliteReaderConnsEnv, test.raw)
			if got := sqliteReaderConns(); got != test.want {
				t.Fatalf("sqliteReaderConns() with %q = %d, want %d", test.raw, got, test.want)
			}
		})
	}
}

// TestOpenSQLiteReaderAppliesResolvedPoolSize covers the wiring: the reader
// pool opens with the resolved size instead of a fixed constant.
func TestOpenSQLiteReaderAppliesResolvedPoolSize(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reader-pool-size.db")
	writer, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	for _, test := range []struct {
		raw  string
		want int
	}{
		{raw: "", want: defaultSQLiteReaderConns},
		{raw: "7", want: 7},
	} {
		t.Setenv(sqliteReaderConnsEnv, test.raw)
		reader, err := OpenSQLiteReader(dbPath)
		if err != nil {
			t.Fatalf("OpenSQLiteReader with %q: %v", test.raw, err)
		}
		got := reader.Stats().MaxOpenConnections
		_ = reader.Close()
		if got != test.want {
			t.Fatalf("reader MaxOpenConnections with %q = %d, want %d", test.raw, got, test.want)
		}
	}
}
