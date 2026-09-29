//go:build windows

package codexdbg

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConfigureProcessTreeSuppressesConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe")

	configureProcessTree(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("configureProcessTree did not set process attributes")
	}
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_SUSPENDED == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_SUSPENDED", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}
