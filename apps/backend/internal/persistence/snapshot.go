package persistence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/common/logger"
)

const (
	// bootStagingDirPrefix marks the private staging directories
	// createPreMigrationBackup reserves before VACUUM INTO writes into them.
	bootStagingDirPrefix = ".kandev-backup-"

	// staleBootStagingAge is how long a boot staging directory must show no
	// sign of writing before it counts as crash debris. A boot backup is a
	// single pre-migration step, so this only has to exceed the slowest
	// plausible upgrade; the freshness check in SweepStaleBootStaging is what
	// actually protects a running backup.
	staleBootStagingAge = 6 * time.Hour
)

// snapshotPath returns the absolute path for a new backup file.
// fromVersion defaults to "pre-meta" when empty.
func snapshotPath(backupDir, fromVersion string) string {
	v := fromVersion
	if v == "" {
		v = "pre-meta"
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	name := fmt.Sprintf("kandev-%s-%s.db", v, ts)
	return filepath.Join(backupDir, name)
}

// SnapshotSQLite copies the live database to path using VACUUM INTO,
// which produces a clean, defragmented snapshot including all WAL frames.
// Returns the size of the created file in bytes. Exported for use by
// internal/system/backups; the lowercase alias preserves call sites in
// this package.
func SnapshotSQLite(writer *sqlx.DB, path string) (int64, error) {
	return snapshotSQLite(writer, path)
}

// SnapshotSQLiteContext creates a cancellable SQLite snapshot.
func SnapshotSQLiteContext(ctx context.Context, writer *sqlx.DB, path string) (int64, error) {
	return snapshotSQLiteContext(ctx, writer, path)
}

// snapshotSQLite copies the live database to path using VACUUM INTO,
// which produces a clean, defragmented snapshot including all WAL frames.
// Returns the size of the created file in bytes.
func snapshotSQLite(writer *sqlx.DB, path string) (int64, error) {
	return snapshotSQLiteContext(context.Background(), writer, path)
}

func snapshotSQLiteContext(ctx context.Context, writer *sqlx.DB, path string) (int64, error) {
	_, statErr := os.Lstat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return 0, fmt.Errorf("inspect snapshot destination %s: %w", path, statErr)
	}
	pathExisted := statErr == nil
	installed := false
	defer func() {
		if !installed && !pathExisted {
			// VACUUM INTO writes the destination incrementally. A canceled or
			// failed statement can leave a partial SQLite file behind, which
			// must not be mistaken for a usable backup on retry.
			_ = os.Remove(path)
		}
	}()
	if _, err := writer.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return 0, fmt.Errorf("vacuum into %s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("snapshot canceled after vacuum into %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat snapshot %s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("snapshot canceled before installing %s: %w", path, err)
	}
	installed = true
	return info.Size(), nil
}

// SweepStaleBootStaging removes boot staging directories left behind by a
// process that died between MkdirTemp and the deferred cleanup, which otherwise
// keeps a whole database copy on disk.
//
// Liveness comes from the newest mtime among the directory and its direct
// children rather than the directory alone: VACUUM INTO writes the staged file
// in place, and a directory's own mtime does not advance while a file inside it
// grows. A staging directory still being written therefore keeps a fresh mtime
// and is never removed. Published snapshots carry no staging prefix and are
// never matched.
func SweepStaleBootStaging(backupDir string, log *logger.Logger) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), bootStagingDirPrefix) {
			continue
		}
		path := filepath.Join(backupDir, e.Name())
		newest, ok := newestModTime(path, e)
		if !ok || time.Since(newest) < staleBootStagingAge {
			continue
		}
		if err := os.RemoveAll(path); err != nil && log != nil {
			log.Warn("persistence: failed to remove stale boot backup staging directory",
				zap.String("path", path), zap.Error(err))
		}
	}
}

// newestModTime returns the most recent modification time among dir itself and
// its direct children. ok is false when the directory cannot be inspected.
func newestModTime(dir string, entry os.DirEntry) (time.Time, bool) {
	info, err := entry.Info()
	if err != nil {
		return time.Time{}, false
	}
	newest := info.ModTime()
	children, err := os.ReadDir(dir)
	if err != nil {
		return newest, true
	}
	for _, child := range children {
		childInfo, err := child.Info()
		if err != nil {
			continue
		}
		if childInfo.ModTime().After(newest) {
			newest = childInfo.ModTime()
		}
	}
	return newest, true
}

// PruneBackups is the exported alias of pruneBackups, intended for callers
// outside this package (e.g. internal/system/backups retention tests) that
// need to verify the auto-snapshot retention policy without copy-pasting
// the implementation.
func PruneBackups(dir string, keep int) error {
	return pruneBackups(dir, keep)
}

// pruneBackups retains only the keep newest backup files (sorted by mtime)
// inside dir, deleting everything older. Files are matched by the
// "kandev-*.db" glob pattern. Non-matching files in the directory are not
// touched. Errors on individual deletes are silently ignored so a stale
// permission issue on one file does not block the rest.
func pruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read backup dir %s: %w", dir, err)
	}

	type fileInfo struct {
		path  string
		mtime time.Time
	}
	var files []fileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "kandev-") || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			path:  filepath.Join(dir, e.Name()),
			mtime: info.ModTime(),
		})
	}

	if len(files) <= keep {
		return nil
	}

	// Sort newest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime.After(files[j].mtime)
	})

	for _, f := range files[keep:] {
		_ = os.Remove(f.path)
	}
	return nil
}
