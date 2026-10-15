//go:build windows

package recoveryartifact

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func filesystemObjectIdentity(path string, info os.FileInfo) (string, bool) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var pinned windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &pinned); err != nil {
		return "", false
	}
	// The opened object must still be a real file or directory of the observed type.
	if pinned.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DEVICE) != 0 ||
		(pinned.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != info.IsDir() {
		return "", false
	}
	index := uint64(pinned.FileIndexHigh)<<32 | uint64(pinned.FileIndexLow)
	return fmt.Sprintf("%d:%d:%d", pinned.VolumeSerialNumber, index, uint32(info.Mode().Type())), true
}
