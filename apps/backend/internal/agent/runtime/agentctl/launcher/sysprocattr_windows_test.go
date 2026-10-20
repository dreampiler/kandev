//go:build windows

package launcher

import (
	"syscall"
	"testing"

	"github.com/kandev/kandev/internal/common/winproc"

	"golang.org/x/sys/windows"
)

func TestBuildSysProcAttrHidesConsoleWindow(t *testing.T) {
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
			attr := buildSysProcAttr(false)
			if attr == nil {
				t.Fatal("buildSysProcAttr returned nil")
			}
			if attr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
				t.Fatalf("CreationFlags = %#x, want CREATE_NEW_PROCESS_GROUP", attr.CreationFlags)
			}
			if !attr.HideWindow {
				t.Fatal("HideWindow = false, want true")
			}
			if got := attr.CreationFlags&windows.CREATE_NO_WINDOW != 0; got != tc.wantNoWindow {
				t.Fatalf("CreationFlags = %#x, CREATE_NO_WINDOW set = %v, want %v", attr.CreationFlags, got, tc.wantNoWindow)
			}
		})
	}
}

func TestBuildSysProcAttrStartsSuspendedForJobAssignment(t *testing.T) {
	base := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED)
	for _, tc := range []struct {
		name          string
		consoleWindow bool
		want          uint32
	}{
		{name: "shares existing console window", consoleWindow: true, want: base},
		{name: "no console window to share", consoleWindow: false, want: base | windows.CREATE_NO_WINDOW},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(winproc.SetConsoleWindowProbeForTest(tc.consoleWindow))
			attr := buildSysProcAttr(false)
			if attr.CreationFlags != tc.want {
				t.Fatalf("CreationFlags = %#x, want %#x", attr.CreationFlags, tc.want)
			}
		})
	}
}
