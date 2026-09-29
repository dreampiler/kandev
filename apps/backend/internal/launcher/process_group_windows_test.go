//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsConfigureManagedProcessSuppressesConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe")

	configureManagedProcess(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("configureManagedProcess did not set process attributes")
	}
	flags := cmd.SysProcAttr.CreationFlags
	if flags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP", flags)
	}
	if flags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", flags)
	}
}

const launcherWindowsHelperEnv = "KANDEV_LAUNCHER_WINDOWS_HELPER"

func TestWindowsManagedProcessForceKillTreatsAlreadyExitedPIDAsGraceful(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestWindowsLauncherHelper")
	cmd.Env = append(os.Environ(), launcherWindowsHelperEnv+"=1")
	configureManagedProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait helper: %v", err)
	}

	proc := &managedProcess{
		label: "already-exited",
		cmd:   cmd,
		done:  make(chan struct{}),
	}
	result := proc.forceKill("test")
	if result.err != nil {
		t.Fatalf("forceKill err = %v, want nil", result.err)
	}
	if !result.graceful || result.forceKilled {
		t.Fatalf("forceKill result graceful=%v forceKilled=%v, want true/false", result.graceful, result.forceKilled)
	}
	if result.pid != pid {
		t.Fatalf("forceKill pid = %d, want %d", result.pid, pid)
	}
}

func TestWindowsLauncherHelper(t *testing.T) {
	if os.Getenv(launcherWindowsHelperEnv) != "1" {
		return
	}
	os.Exit(0)
}
