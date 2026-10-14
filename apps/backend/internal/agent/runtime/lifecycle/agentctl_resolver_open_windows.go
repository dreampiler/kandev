//go:build windows

package lifecycle

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const (
	cachedHelperShareRetryWindow = 2 * time.Second
	cachedHelperShareRetryDelay  = 25 * time.Millisecond
)

// openCachedHelperForRead opens a cached helper for verification. When another
// process publishes the same helper concurrently, Windows briefly reports a
// sharing violation or access denied for the path while that rename holds it,
// so those errors are retried for a short window instead of failing the
// resolution. os.Open keeps Go's path handling, including long paths.
func openCachedHelperForRead(path string) (*os.File, error) {
	deadline := time.Now().Add(cachedHelperShareRetryWindow)
	for {
		file, err := os.Open(path)
		if err == nil {
			return file, nil
		}
		transient := errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
		if !transient || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(cachedHelperShareRetryDelay)
	}
}
