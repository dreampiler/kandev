//go:build !windows

package winproc

import "os/exec"

// HideConsoleWindow is a no-op on platforms that have no Windows console
// windows.
func HideConsoleWindow(_ *exec.Cmd) {}
