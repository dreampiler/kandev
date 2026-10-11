//go:build windows

package hostutility

import (
	"context"
	"syscall"
	"testing"
)

func TestHostNPMConfigCommandHidesConsoleWindow(t *testing.T) {
	cmd := hostNPMConfigCommand(context.Background(), "cache")
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
