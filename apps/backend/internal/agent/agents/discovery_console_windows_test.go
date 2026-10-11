//go:build windows

package agents

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
)

func assertHiddenConsoleCommand(t *testing.T, cmd *exec.Cmd, label string) {
	t.Helper()
	if cmd.SysProcAttr == nil {
		t.Fatalf("%s: SysProcAttr = nil", label)
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatalf("%s: HideWindow = false, want true", label)
	}
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("%s: CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP", label, cmd.SysProcAttr.CreationFlags)
	}
}

func TestDiscoveryCommandsHideConsoleWindow(t *testing.T) {
	ctx := context.Background()
	assertHiddenConsoleCommand(t, commandCheckCommand(ctx, "hermes", []string{"acp", "--check"}), "hermes check")
	assertHiddenConsoleCommand(t, openCodeVersionCommand(ctx, "opencode"), "opencode version")
	assertHiddenConsoleCommand(t, openCodeNPMCacheCommand(ctx), "npm cache")
}
