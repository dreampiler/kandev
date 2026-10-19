//go:build windows

package launcher

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"github.com/kandev/kandev/internal/agentctl/server/winproc"
	commonwinproc "github.com/kandev/kandev/internal/common/winproc"
	"golang.org/x/sys/windows"
)

func ignoreBrokenPipeSignal() {
}

func configureManagedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func killManagedProcessGroup(pid int) error {
	err := runLauncherTaskkill("/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	if err != nil && managedProcessHasExited(pid) {
		return syscall.ESRCH
	}
	return err
}

// Check the process handle when taskkill's localized output cannot establish
// that an already-reaped root is gone. Access failures remain unknown.
func managedProcessHasExited(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	result, err := windows.WaitForSingleObject(process, 0)
	return err == nil && result == windows.WAIT_OBJECT_0
}

func managedProcessGracefulScope(_ bool) gracefulSignalScope {
	return gracefulSignalScope("cooperative-or-tree-fallback")
}

func terminateManagedProcess(pid, _ int, _ bool) error {
	if err := commonwinproc.RequestShutdown(pid); err == nil {
		return nil
	}
	// The dev backend is launched through make on Windows. Signalling only make
	// would orphan the backend, so retain tree-wide graceful cleanup there.
	return terminateManagedProcessGroup(pid)
}

func terminateManagedProcessGroup(pid int) error {
	return runLauncherTaskkill("/T", "/PID", fmt.Sprintf("%d", pid))
}

func isManagedProcessTarget(_, _ int) bool {
	return false
}

func managedProcessGroupCleanupSupported() bool {
	return true
}

func runLauncherTaskkill(args ...string) error {
	return winproc.RunTaskkill(args...)
}
