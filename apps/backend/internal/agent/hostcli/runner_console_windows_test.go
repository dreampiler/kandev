//go:build windows

package hostcli

import (
	"context"
	"syscall"
	"testing"
)

func TestExecRunnerCommandHidesConsoleWindow(t *testing.T) {
	cmd := execRunnerCommand(context.Background(), []string{"claude", "--version"})
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil, want set")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false, want true")
	}
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP", cmd.SysProcAttr.CreationFlags)
	}
}
