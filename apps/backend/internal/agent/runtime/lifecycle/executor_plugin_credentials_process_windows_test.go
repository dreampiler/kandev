//go:build windows

package lifecycle

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/kandev/kandev/internal/common/winproc"
	"golang.org/x/sys/windows"
)

// Assign the shell before it runs so every pipe-holding descendant is owned.
func startOwnedCredentialShell(cmd *exec.Cmd) (func(context.Context) error, func() error, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	job, err := winproc.InstallKillOnCloseJobForSuspendedCommand(cmd)
	if err != nil {
		// Job setup terminates its suspended child on failure. Reap that exact
		// command even when setup could not finish its own termination attempt.
		_ = cmd.Process.Kill()
		return nil, nil, errors.Join(err, cmd.Wait())
	}
	stop := func(ctx context.Context) error {
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		// Close remains necessary on an error: kill-on-close owns the whole tree.
		return errors.Join(job.TerminateAndWait(cleanupCtx), job.Close())
	}
	return stop, job.Close, nil
}
