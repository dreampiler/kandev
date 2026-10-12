package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

// TestMain enforces no goroutine leaks across the lifecycle package. The
// Manager owns a remote-status polling loop registered on m.wg plus
// detached helpers (stream connectors, agentctl-ready watcher, passthrough
// stderr poller) spawned across manager_*.go and session.go. Regressions
// where a Stop path returns before background work drains surface here.
//
// OTel note: `agentctl/tracing.initTracing` lazily inits the SDK provider
// only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set — in tests it isn't, so
// the noop provider is used and no batchSpanProcessor / OTLP retry
// goroutines are spawned. No IgnoreTopFunction is needed here.
//
// sshKeepaliveInterval/sshKeepaliveDeadline are zeroed for this test binary
// so the fifteen CreateInstance/ResumeRemoteInstance call sites across the
// package don't each acquire a real 15s SSH transport watchdog under this
// leak check. A non-positive value disables the watchdog
// (sshKeepaliveTuningValid); tests that exercise it set millisecond values
// of their own and must not run with t.Parallel().
//
// The user profile is pinned to a temporary directory for the whole binary.
// Code under test resolves it through os.UserHomeDir (HOME, or USERPROFILE
// on Windows) and os.UserCacheDir (XDG_CACHE_HOME, or LOCALAPPDATA on
// Windows); a test that forgets setTestUserHome must not read or write the
// developer's real SSH config, agent credentials or caches. The Go toolchain
// locations that derive from the same variables are resolved first, so tests
// that build helpers keep the developer's build and module caches.
func TestMain(m *testing.M) {
	sshKeepaliveInterval = 0
	sshKeepaliveDeadline = 0

	keepGoToolchainLocations()
	profile, err := os.MkdirTemp("", "lifecycle-test-profile-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create test profile: %v\n", err)
		os.Exit(1)
	}
	for _, name := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "XDG_CACHE_HOME"} {
		if err := os.Setenv(name, profile); err != nil {
			fmt.Fprintf(os.Stderr, "pin %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(profile)
	if code == 0 {
		if err := goleak.Find(); err != nil {
			fmt.Fprintf(os.Stderr, "goleak: errors on successful test run: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}

// keepGoToolchainLocations pins GOCACHE, GOPATH and GOENV to the defaults the
// go command would derive from the real profile, when they are not already set.
func keepGoToolchainLocations() {
	defaults := map[string]func() (string, error){
		"GOCACHE": func() (string, error) {
			dir, err := os.UserCacheDir()
			return filepath.Join(dir, "go-build"), err
		},
		"GOPATH": func() (string, error) {
			dir, err := os.UserHomeDir()
			return filepath.Join(dir, "go"), err
		},
		"GOENV": func() (string, error) {
			dir, err := os.UserConfigDir()
			return filepath.Join(dir, "go", "env"), err
		},
	}
	for name, resolve := range defaults {
		if os.Getenv(name) != "" {
			continue
		}
		if value, err := resolve(); err == nil {
			_ = os.Setenv(name, value)
		}
	}
}
