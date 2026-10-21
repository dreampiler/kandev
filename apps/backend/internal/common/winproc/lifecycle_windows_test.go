//go:build windows

package winproc

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// These tests start no process: they observe the resume path through the
// ntResumeProcess seam and the caller's own process handle.

func replaceNtResumeProcess(t *testing.T, fake func(windows.Handle) error) {
	t.Helper()
	original := ntResumeProcess
	ntResumeProcess = fake
	t.Cleanup(func() { ntResumeProcess = original })
}

func TestResumeSuspendedProcessResumesThroughOneProcessHandle(t *testing.T) {
	var handles []windows.Handle
	replaceNtResumeProcess(t, func(process windows.Handle) error {
		handles = append(handles, process)
		return nil
	})

	require.NoError(t, ResumeSuspendedProcess(os.Getpid()))

	require.Len(t, handles, 1)
	require.NotZero(t, handles[0])
}

func TestResumeSuspendedProcessReportsResumeFailure(t *testing.T) {
	replaceNtResumeProcess(t, func(windows.Handle) error {
		return windows.STATUS_ACCESS_DENIED
	})

	err := ResumeSuspendedProcess(os.Getpid())

	require.Error(t, err)
	require.True(t, errors.Is(err, windows.STATUS_ACCESS_DENIED))
	require.Contains(t, err.Error(), "resume pid")
}

func TestResumeSuspendedProcessRejectsUnopenableProcess(t *testing.T) {
	called := false
	replaceNtResumeProcess(t, func(windows.Handle) error {
		called = true
		return nil
	})

	require.Error(t, ResumeSuspendedProcess(0))
	require.False(t, called)
}

func TestNtResumeProcessIsAvailable(t *testing.T) {
	require.NoError(t, procNtResumeProcess.Find())
}
