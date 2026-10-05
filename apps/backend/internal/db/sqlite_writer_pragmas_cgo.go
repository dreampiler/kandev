//go:build cgo

package db

import "github.com/mattn/go-sqlite3"

// applyWriterPragmas runs on every writer connection. journal_size_limit is
// per-connection state and a pooled connection can be replaced at any time, so
// it is set here rather than once per database handle.
func applyWriterPragmas(conn *sqlite3.SQLiteConn) error {
	_, err := conn.Exec(journalSizeLimitSQL, nil)
	return err
}
