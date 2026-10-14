//go:build !windows

package lifecycle

import "os"

// openCachedHelperForRead opens a cached helper. POSIX rename already lets a
// concurrent publish replace a file that is open for reading.
func openCachedHelperForRead(path string) (*os.File, error) {
	return os.Open(path)
}
