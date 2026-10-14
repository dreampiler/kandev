//go:build windows

package lifecycle

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOpenCachedHelperForReadWaitsOutTransientSharingViolation(t *testing.T) {
	cached := filepath.Join(t.TempDir(), "agentctl")
	if err := os.WriteFile(cached, []byte("helper"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hold the file with no sharing, as a concurrent publish does while it
	// replaces the path, so the first open attempts see a sharing violation.
	name, err := windows.UTF16PtrFromString(cached)
	if err != nil {
		t.Fatal(err)
	}
	exclusive, err := windows.CreateFile(name, windows.GENERIC_READ|windows.DELETE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("hold helper exclusively: %v", err)
	}
	if _, err := os.Open(cached); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("plain open while held = %v, want a sharing violation", err)
	}

	opened := make(chan error, 1)
	go func() {
		reader, err := openCachedHelperForRead(cached)
		if err == nil {
			data, readErr := io.ReadAll(reader)
			_ = reader.Close()
			if readErr == nil && string(data) != "helper" {
				readErr = errors.New("unexpected helper content " + string(data))
			}
			err = readErr
		}
		opened <- err
	}()
	// Release well inside the retry window, after the reader has started
	// hitting the violation.
	time.Sleep(100 * time.Millisecond)
	if err := windows.CloseHandle(exclusive); err != nil {
		t.Fatal(err)
	}
	if err := <-opened; err != nil {
		t.Fatalf("open after the transient hold: %v", err)
	}
}

func TestOpenCachedHelperForReadOpensLongPath(t *testing.T) {
	dir := t.TempDir()
	for len(dir) <= 300 {
		dir = filepath.Join(dir, strings.Repeat("d", 50))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cached := filepath.Join(dir, "agentctl")
	if err := os.WriteFile(cached, []byte("helper"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := openCachedHelperForRead(cached)
	if err != nil {
		t.Fatalf("open long cache path: %v", err)
	}
	_ = reader.Close()
}

func TestOpenCachedHelperForReadReportsMissingFile(t *testing.T) {
	_, err := openCachedHelperForRead(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing helper error = %v, want os.ErrNotExist", err)
	}
}
