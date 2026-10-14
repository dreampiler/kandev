//go:build windows

package lifecycle

import (
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
// resolution. The share mode does not block other readers or deleters.
func openCachedHelperForRead(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	deadline := time.Now().Add(cachedHelperShareRetryWindow)
	for {
		handle, err := windows.CreateFile(
			name,
			windows.GENERIC_READ,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err == nil {
			return os.NewFile(uintptr(handle), path), nil
		}
		transient := err == windows.ERROR_SHARING_VIOLATION || err == windows.ERROR_ACCESS_DENIED
		if !transient || time.Now().After(deadline) {
			return nil, &os.PathError{Op: "open", Path: path, Err: err}
		}
		time.Sleep(cachedHelperShareRetryDelay)
	}
}
