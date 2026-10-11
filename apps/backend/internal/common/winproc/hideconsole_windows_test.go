//go:build windows

package winproc

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestHideConsoleWindowSetsHiddenFlags(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo")
	HideConsoleWindow(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil, want set")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false, want true")
	}
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW != 0 {
		t.Fatalf("CreationFlags = %#x, must not include CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}

func TestHideConsoleWindowPreservesCallerFlags(t *testing.T) {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.BELOW_NORMAL_PRIORITY_CLASS}
	HideConsoleWindow(cmd)
	if cmd.SysProcAttr.CreationFlags&windows.BELOW_NORMAL_PRIORITY_CLASS == 0 {
		t.Fatalf("caller flag lost: CreationFlags = %#x", cmd.SysProcAttr.CreationFlags)
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false, want true")
	}
}
