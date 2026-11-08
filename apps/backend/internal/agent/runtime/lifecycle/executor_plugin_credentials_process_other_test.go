//go:build !windows

package lifecycle

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

func startOwnedCredentialShell(cmd *exec.Cmd) (func(context.Context) error, func() error, error) {
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	stop := func(context.Context) error {
		err := cmd.Process.Kill()
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	return stop, func() error { return nil }, nil
}
