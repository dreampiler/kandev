package requiredstores

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/startup"
)

// newWriterReaderPair opens a real SQLite writer/reader pair on one file, the
// same way the backend does, so the writer carries the commit hook.
func newWriterReaderPair(t *testing.T) (*sqlx.DB, *sqlx.DB, *db.Pool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kandev.db")
	rawWriter, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	writer := sqlx.NewDb(rawWriter, "sqlite3")
	if _, err := writer.Exec("CREATE TABLE required_probe (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)"); err != nil {
		t.Fatalf("create required table: %v", err)
	}
	rawReader, err := db.OpenSQLiteReader(path)
	if err != nil {
		t.Fatalf("OpenSQLiteReader: %v", err)
	}
	reader := sqlx.NewDb(rawReader, "sqlite3")
	pool := db.NewPool(writer, reader)
	t.Cleanup(func() { _ = pool.Close() })
	return writer, reader, pool
}

func newProgressHealth(t *testing.T, pool *db.Pool, tables ...string) (*Tracker, *Health) {
	t.Helper()
	tracker, err := NewTracker([]Descriptor{{
		ID: "fixture", OwnerPackage: "fixture", RequiredTables: tables,
		Sweep: startup.StepStoresRepositories,
	}})
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	if err := tracker.RecordSuccess("fixture"); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	health := NewHealth(tracker, pool, nil)
	health.probeTimeout = 100 * time.Millisecond
	health.SetInterval(400 * time.Millisecond)
	return tracker, health
}

// committingWriter holds the single writer connection and keeps committing on
// it, the shape of sustained agent write load: the pool never frees the
// connection for the probe, yet every write succeeds.
type committingWriter struct {
	conn    *sqlx.Conn
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func holdCommittingWriter(t *testing.T, writer *sqlx.DB) *committingWriter {
	t.Helper()
	conn, err := writer.Connx(context.Background())
	if err != nil {
		t.Fatalf("hold writer connection: %v", err)
	}
	w := &committingWriter{conn: conn, stop: make(chan struct{}), stopped: make(chan struct{})}
	go func() {
		defer close(w.stopped)
		for {
			select {
			case <-w.stop:
				return
			default:
			}
			if _, err := conn.ExecContext(context.Background(), "INSERT INTO required_probe (v) VALUES ('x')"); err != nil {
				t.Errorf("committing write: %v", err)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	t.Cleanup(func() { w.stopCommitting(); _ = conn.Close() })
	return w
}

// stopCommitting stops the writes but keeps the connection held, which turns
// the writer from busy into stuck.
func (w *committingWriter) stopCommitting() {
	w.once.Do(func() { close(w.stop) })
	<-w.stopped
}

func TestHealthWriterBusyWithCommitsStaysHealthy(t *testing.T) {
	writer, _, pool := newWriterReaderPair(t)
	_, health := newProgressHealth(t, pool, "required_probe")
	if err := health.Check(context.Background()); err != nil {
		t.Fatalf("initial Check: %v", err)
	}

	busy := holdCommittingWriter(t, writer)
	time.Sleep(50 * time.Millisecond)
	before := writer.Stats()
	for attempt := 1; attempt <= runtimeFailureThreshold+1; attempt++ {
		if deferred, err := health.checkRuntime(context.Background()); deferred || err != nil {
			t.Fatalf("probe %d with a busy committing writer = deferred %v, err %v; want healthy", attempt, deferred, err)
		}
	}
	if err := health.Check(context.Background()); err != nil {
		t.Fatalf("strict Check with a busy committing writer: %v", err)
	}
	if after := writer.Stats(); after.WaitCount == before.WaitCount {
		t.Fatal("writer ping never queued; the busy-writer path was not exercised")
	}
	if !health.Healthy() {
		t.Fatal("a writer that keeps committing was reported unavailable")
	}

	// The same held connection without commits is a stuck writer and must
	// still fail once the progress window has passed.
	busy.stopCommitting()
	time.Sleep(500 * time.Millisecond)
	for attempt := 1; attempt <= runtimeFailureThreshold; attempt++ {
		diagnostic, err := health.probe(context.Background(), true)
		if err == nil {
			t.Fatalf("probe %d with a stuck writer returned nil", attempt)
		}
		if diagnostic == nil || diagnostic.stage != "writer_ping" || diagnostic.errorClass != "deadline_exceeded" {
			t.Fatalf("stuck writer diagnostic = %#v, want writer_ping deadline_exceeded", diagnostic)
		}
	}
	if health.Healthy() {
		t.Fatalf("health stayed healthy after %d probes against a stuck writer", runtimeFailureThreshold)
	}
}

func TestHealthMissingTableUnhealthyWhileWriterCommits(t *testing.T) {
	writer, _, pool := newWriterReaderPair(t)
	_, health := newProgressHealth(t, pool, "required_probe", "missing_table")
	holdCommittingWriter(t, writer)
	time.Sleep(50 * time.Millisecond)

	diagnostic, err := health.check(context.Background())
	if err == nil {
		t.Fatal("Check returned nil with a missing required table")
	}
	if diagnostic == nil || diagnostic.stage != "table_probe" || diagnostic.errorClass != "required_table_missing" {
		t.Fatalf("diagnostic = %#v, want table_probe required_table_missing", diagnostic)
	}
	if health.Healthy() {
		t.Fatal("a missing required table did not mark the store unhealthy")
	}
}

func TestHealthClosedWriterUnhealthyDespiteRecentCommits(t *testing.T) {
	writer, _, pool := newWriterReaderPair(t)
	_, health := newProgressHealth(t, pool, "required_probe")
	if _, err := writer.Exec("INSERT INTO required_probe (v) VALUES ('recent')"); err != nil {
		t.Fatalf("recent commit: %v", err)
	}
	if !health.writerCommittedSince(time.Now().Add(-time.Second)) {
		t.Fatal("writer commit hook did not record the recent commit")
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	diagnostic, err := health.check(context.Background())
	if err == nil {
		t.Fatal("Check returned nil with a closed writer")
	}
	if diagnostic == nil || diagnostic.stage != "writer_ping" || diagnostic.errorClass != "probe_error" {
		t.Fatalf("diagnostic = %#v, want writer_ping probe_error", diagnostic)
	}
	if health.Healthy() {
		t.Fatal("a closed writer was reported healthy because of an earlier commit")
	}
}
