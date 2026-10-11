//go:build windows

package usage

import (
	"context"
	"syscall"
	"testing"
)

func TestAntigravityUsageCommandHidesConsoleWindow(t *testing.T) {
	cmd := antigravityUsageCommand(context.Background())
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

func TestAntigravityUsageCommandDisablesAutoUpdate(t *testing.T) {
	cmd := antigravityUsageCommand(context.Background())
	want := antigravityDisableAutoUpdateEnv + "=true"
	for _, entry := range cmd.Env {
		if entry == want {
			return
		}
	}
	t.Fatalf("child environment is missing %s", want)
}
