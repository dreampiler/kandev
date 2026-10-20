//go:build windows

package mcpconfig

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/kandev/kandev/internal/common/winproc"
	"golang.org/x/sys/windows"
)

func TestPrepareNativeMCPProcessHidesConsoleWindow(t *testing.T) {
	for _, tc := range []struct {
		name          string
		consoleWindow bool
		wantNoWindow  bool
	}{
		{name: "shares existing console window", consoleWindow: true, wantNoWindow: false},
		{name: "no console window to share", consoleWindow: false, wantNoWindow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(winproc.SetConsoleWindowProbeForTest(tc.consoleWindow))
			cmd := exec.Command(os.Args[0], "mcp", "list")
			cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.BELOW_NORMAL_PRIORITY_CLASS}
			if err := prepareNativeMCPProcess(cmd); err != nil {
				t.Fatalf("prepareNativeMCPProcess: %v", err)
			}
			flags := cmd.SysProcAttr.CreationFlags
			for _, want := range []uint32{
				windows.BELOW_NORMAL_PRIORITY_CLASS,
				syscall.CREATE_NEW_PROCESS_GROUP,
				windows.CREATE_SUSPENDED,
			} {
				if flags&want == 0 {
					t.Fatalf("CreationFlags = %#x, want %#x set", flags, want)
				}
			}
			if !cmd.SysProcAttr.HideWindow {
				t.Fatal("HideWindow = false, want true")
			}
			if got := flags&windows.CREATE_NO_WINDOW != 0; got != tc.wantNoWindow {
				t.Fatalf("CreationFlags = %#x, CREATE_NO_WINDOW set = %v, want %v", flags, got, tc.wantNoWindow)
			}
		})
	}

	console := exec.Command(os.Args[0], "mcp", "list")
	console.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	if err := prepareNativeMCPProcess(console); err == nil {
		t.Fatal("prepareNativeMCPProcess accepted CREATE_NEW_CONSOLE")
	}
}
