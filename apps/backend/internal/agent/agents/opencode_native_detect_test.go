package agents

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/managedruntime"
)

type versionRun struct {
	output string
	err    error
}

// fakeOpenCodeNative scripts the version runs of one detector and records the
// retry waits, so no test runs a real executable or sleeps.
type fakeOpenCodeNative struct {
	mu    sync.Mutex
	runs  []versionRun
	calls []string
	waits []time.Duration
	now   time.Time
}

func newFakeOpenCodeDetector(fake *fakeOpenCodeNative, path string) *openCodeNativeDetector {
	d := newOpenCodeNativeDetector()
	d.lookPath = func(string) (string, error) { return path, nil }
	d.runVersion = func(_ context.Context, path string) ([]byte, error) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.calls = append(fake.calls, path)
		if len(fake.runs) == 0 {
			return nil, errors.New("unexpected version run")
		}
		run := fake.runs[0]
		fake.runs = fake.runs[1:]
		return []byte(run.output), run.err
	}
	d.sleep = func(_ context.Context, wait time.Duration) error {
		fake.waits = append(fake.waits, wait)
		return nil
	}
	d.now = func() time.Time { return fake.now }
	return d
}

var errExitStatus1 = errors.New("exit status 1")

func TestOpenCodeNativeDetectionRetriesTransientFailures(t *testing.T) {
	fake := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{
		{err: errExitStatus1},
		{output: "Bun is warming up"},
		{output: "1.18.34\n"},
	}}
	got, found, err := newFakeOpenCodeDetector(fake, "/bin/opencode").detect(context.Background())
	if err != nil || !found {
		t.Fatalf("detect = %+v, found %v, err %v; want the third run's version", got, found, err)
	}
	if got.Version != "1.18.34" || got.Family != managedruntime.OpenCodeFamilyV1 {
		t.Fatalf("runtime = %+v, want v1 1.18.34", got)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("version runs = %d, want 3", len(fake.calls))
	}
	if want := []time.Duration{300 * time.Millisecond, time.Second}; !equalDurations(fake.waits, want) {
		t.Fatalf("retry waits = %v, want %v", fake.waits, want)
	}
}

func TestOpenCodeNativeDetectionReportsBoundedOutputAfterRetries(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory to redact")
	}
	output := "error: cannot start runtime at " + home + "\x1b[0m\n" + strings.Repeat("x", 400)
	fake := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{
		{output: output, err: errExitStatus1},
		{output: output, err: errExitStatus1},
		{output: output, err: errExitStatus1},
	}}
	_, found, err := newFakeOpenCodeDetector(fake, "/bin/opencode").detect(context.Background())
	if err == nil || !found {
		t.Fatalf("detect found %v, err %v; want a found executable and an error", found, err)
	}
	message := err.Error()
	if len(fake.calls) != 3 {
		t.Fatalf("version runs = %d, want 3", len(fake.calls))
	}
	if !strings.Contains(message, "read native OpenCode version: exit status 1") ||
		!strings.Contains(message, `output: "error: cannot start runtime at ~ [0m `) {
		t.Fatalf("error = %q, want the exit status and a sanitized output excerpt", message)
	}
	if strings.Contains(message, home) || strings.Contains(message, "\x1b") || strings.Contains(message, strings.Repeat("x", 201)) {
		t.Fatalf("error = %q, want home redacted, control bytes dropped and the excerpt bounded", message)
	}
}

func TestOpenCodeNativeDetectionMarksTimedOutRuns(t *testing.T) {
	d := newOpenCodeNativeDetector()
	d.lookPath = func(string) (string, error) { return "/bin/opencode", nil }
	d.versionTimeout = 10 * time.Millisecond
	d.backoff = nil
	d.runVersion = func(ctx context.Context, _ string) ([]byte, error) {
		<-ctx.Done()
		return nil, errExitStatus1
	}
	_, _, err := d.detect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exit status 1 (timed out after 10ms)") {
		t.Fatalf("error = %v, want the killed run marked as timed out", err)
	}
}

func TestOpenCodeNativeDetectionFallsBackToRecentDetectionOfTheSamePath(t *testing.T) {
	fake := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{
		{output: "opencode 1.18.33"},
		{err: errExitStatus1},
	}}
	d := newFakeOpenCodeDetector(fake, "/bin/opencode")
	if _, _, err := d.detect(context.Background()); err != nil {
		t.Fatalf("first detect: %v", err)
	}
	fake.now = fake.now.Add(9 * time.Minute)
	got, found, err := d.detect(context.Background())
	if err != nil || !found || got.Version != "1.18.33" {
		t.Fatalf("detect after a transient failure = %+v, found %v, err %v; want the cached 1.18.33", got, found, err)
	}
	if len(fake.calls) != 2 || len(fake.waits) != 0 {
		t.Fatalf("runs = %d waits = %v, want one failed run answered from the cache", len(fake.calls), fake.waits)
	}
}

func TestOpenCodeNativeDetectionCacheExpiresAndIsPerPath(t *testing.T) {
	fake := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{
		{output: "opencode 1.18.33"},
		{err: errExitStatus1}, {err: errExitStatus1}, {err: errExitStatus1},
	}}
	d := newFakeOpenCodeDetector(fake, "/bin/opencode")
	if _, _, err := d.detect(context.Background()); err != nil {
		t.Fatalf("first detect: %v", err)
	}
	fake.now = fake.now.Add(11 * time.Minute)
	if _, _, err := d.detect(context.Background()); err == nil {
		t.Fatal("detect after the cache expired succeeded, want the failure")
	}

	other := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{
		{output: "opencode 1.18.33"},
		{err: errExitStatus1}, {err: errExitStatus1}, {err: errExitStatus1},
	}}
	d = newFakeOpenCodeDetector(other, "/bin/opencode")
	if _, _, err := d.detect(context.Background()); err != nil {
		t.Fatalf("first detect: %v", err)
	}
	d.lookPath = func(string) (string, error) { return "/other/opencode", nil }
	if _, _, err := d.detect(context.Background()); err == nil {
		t.Fatal("another executable answered from the first one's cache")
	}
}

func TestOpenCodeNativeDetectionDoesNotRetryDefiniteAnswers(t *testing.T) {
	fake := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{{output: "opencode 3.0.0"}}}
	_, found, err := newFakeOpenCodeDetector(fake, "/bin/opencode").detect(context.Background())
	if err == nil || !found || err.Error() != "native OpenCode major 3 is not supported" {
		t.Fatalf("detect found %v, err %v; want the unsupported major", found, err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("version runs = %d, want 1", len(fake.calls))
	}

	missing := newOpenCodeNativeDetector()
	missing.lookPath = func(name string) (string, error) {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	missing.runVersion = func(context.Context, string) ([]byte, error) {
		t.Fatal("version ran without an executable")
		return nil, nil
	}
	if got, found, err := missing.detect(context.Background()); err != nil || found || got != (OpenCodeNativeRuntime{}) {
		t.Fatalf("detect without an executable = %+v, found %v, err %v; want absent without error", got, found, err)
	}
}

func TestOpenCodeNativeDetectionStopsRetryingWhenTheCallerGivesUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeOpenCodeNative{now: time.Unix(1_000, 0), runs: []versionRun{{err: errExitStatus1}, {output: "opencode 1.18.33"}}}
	d := newFakeOpenCodeDetector(fake, "/bin/opencode")
	d.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, _, err := d.detect(ctx); err == nil {
		t.Fatal("detect succeeded after the caller cancelled")
	}
	if len(fake.calls) != 1 {
		t.Fatalf("version runs = %d, want no retry after cancellation", len(fake.calls))
	}
}

func equalDurations(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
