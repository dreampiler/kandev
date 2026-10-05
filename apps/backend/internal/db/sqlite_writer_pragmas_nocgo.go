//go:build !cgo

package db

import "github.com/mattn/go-sqlite3"

// applyWriterPragmas is a no-op without cgo: go-sqlite3 then ships only a mock
// driver that cannot open a database, and its connection type has no Exec.
// Binaries cross-built with CGO_ENABLED=0 (the linux and darwin agentctl
// helpers) import this package transitively but never open SQLite.
func applyWriterPragmas(*sqlite3.SQLiteConn) error { return nil }
