package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

const (
	defaultBusyTimeout = 5 * time.Second

	// sqliteWriterDriverName registers a writer-only driver whose every
	// connection applies the writer pragmas that have no DSN parameter. It
	// stays a separate driver name because sqlx is told the dialect name
	// separately (sqlx.NewDb) while database/sql keys its driver registry by
	// this name.
	sqliteWriterDriverName = "sqlite3_kandev_writer"

	// journalSizeLimitSQL caps the WAL file size that survives a WAL reset.
	// A checkpoint alone rewrites the WAL in place without shrinking it, so
	// without this bound the file stays at its high-water mark for the life of
	// the database. The limit is a reset-time size bound, not a per-write
	// budget: the WAL still grows freely up to it between resets. 67108864 bytes
	// is 64 MiB, which leaves room for a write burst over the ~4 MiB default
	// autocheckpoint while bounding a single file.
	journalSizeLimitSQL = "PRAGMA journal_size_limit = 67108864"

	// defaultSQLiteReaderConns is the number of concurrent read connections.
	// SQLite WAL mode allows many readers alongside a single writer. The
	// previous default of 4 saturated once 13-20 agent sessions ran at the
	// same time: readers queued on the pool, the required-store health probe
	// timed out, and stateful routes answered 503 until the queue drained.
	defaultSQLiteReaderConns = 12

	// maxSQLiteReaderConns caps the sqliteReaderConnsEnv override.
	maxSQLiteReaderConns = 64

	// sqliteReaderConnsEnv overrides defaultSQLiteReaderConns at startup.
	sqliteReaderConnsEnv = "KANDEV_SQLITE_READER_CONNS"
)

// applyWriterPragmas runs on every writer connection. journal_size_limit is
// per-connection state and a pooled connection can be replaced at any time, so
// it is set here rather than once per database handle.
func applyWriterPragmas(conn *sqlite3.SQLiteConn) error {
	_, err := conn.Exec(journalSizeLimitSQL, nil)
	return err
}

func init() {
	sql.Register(sqliteWriterDriverName, &sqlite3.SQLiteDriver{
		ConnectHook: applyWriterPragmas,
	})
}

// sqliteReaderConns resolves the read pool size. A missing, non-numeric, or
// non-positive override keeps the default; larger values are capped.
func sqliteReaderConns() int {
	raw := strings.TrimSpace(os.Getenv(sqliteReaderConnsEnv))
	if raw == "" {
		return defaultSQLiteReaderConns
	}
	conns, err := strconv.Atoi(raw)
	if err != nil || conns < 1 {
		return defaultSQLiteReaderConns
	}
	if conns > maxSQLiteReaderConns {
		return maxSQLiteReaderConns
	}
	return conns
}

// OpenSQLite opens a SQLite database configured for writes (single connection).
func OpenSQLite(dbPath string) (*sql.DB, error) {
	normalizedPath := normalizeSQLitePath(dbPath)
	if err := ensureSQLiteDir(normalizedPath); err != nil {
		return nil, fmt.Errorf("failed to prepare database path: %w", err)
	}
	if err := ensureSQLiteFile(normalizedPath); err != nil {
		return nil, fmt.Errorf("failed to create database file: %w", err)
	}

	// Writer DSN settings:
	// - foreign_keys=on: enforce FK constraints consistently.
	// - busy_timeout: wait briefly on locks to reduce transient "database is locked".
	// - journal_mode=WAL: better read concurrency with a single writer.
	// - synchronous=NORMAL: reasonable durability/perf tradeoff for app workloads.
	// - cache=shared: allow multiple connections to share a page cache.
	dsn := fmt.Sprintf(
		"file:%s?_foreign_keys=on&_mode=rwc&_busy_timeout=%d&_journal_mode=WAL&_synchronous=NORMAL&_cache=shared",
		normalizedPath,
		int(defaultBusyTimeout/time.Millisecond),
	)
	db, err := sql.Open(sqliteWriterDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Single writer connection: serializes writes and avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return db, nil
}

// OpenSQLiteReader opens a read-only SQLite connection pool with multiple
// concurrent connections. Combined with WAL mode, this allows readers to
// proceed without blocking on (or being blocked by) writes.
func OpenSQLiteReader(dbPath string) (*sql.DB, error) {
	normalizedPath := normalizeSQLitePath(dbPath)

	// Reader DSN: read-only mode and FK enforcement.
	// Do not enable SQLite shared-cache mode: an active reader in that cache
	// can make a later reader observe its stale snapshot after a writer commits.
	// journal_mode and synchronous are database-level (set by the writer).
	dsn := fmt.Sprintf(
		"file:%s?_foreign_keys=on&mode=ro&_busy_timeout=%d",
		normalizedPath,
		int(defaultBusyTimeout/time.Millisecond),
	)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open read-only database: %w", err)
	}

	readerConns := sqliteReaderConns()
	db.SetMaxOpenConns(readerConns)
	db.SetMaxIdleConns(readerConns)

	return db, nil
}

func ensureSQLiteDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func ensureSQLiteFile(dbPath string) error {
	file, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

func normalizeSQLitePath(dbPath string) string {
	if dbPath == "" {
		return dbPath
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return dbPath
	}
	return abs
}
