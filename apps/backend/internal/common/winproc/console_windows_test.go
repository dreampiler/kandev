//go:build windows

package winproc

import (
	"testing"

	"golang.org/x/sys/windows"
)

func withConsoleWindow(t *testing.T, present bool) {
	t.Helper()
	t.Cleanup(SetConsoleWindowProbeForTest(present))
}

func TestNoConsoleWindowFlagKeepsInheritedConsole(t *testing.T) {
	withConsoleWindow(t, true)
	if got := NoConsoleWindowFlag(); got != 0 {
		t.Fatalf("NoConsoleWindowFlag() = %#x with a console window, want 0", got)
	}
}

func TestNoConsoleWindowFlagSuppressesNewConsoleWindow(t *testing.T) {
	withConsoleWindow(t, false)
	if got := NoConsoleWindowFlag(); got != windows.CREATE_NO_WINDOW {
		t.Fatalf("NoConsoleWindowFlag() = %#x without a console window, want CREATE_NO_WINDOW", got)
	}
}
