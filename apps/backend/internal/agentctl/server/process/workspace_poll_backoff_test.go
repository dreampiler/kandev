package process

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// slowBackoffSchedule is the slow-mode wait after each consecutive unchanged
// scan: three waits at the base interval, then doubling up to a 5 minute cap.
var slowBackoffSchedule = []time.Duration{
	30 * time.Second, 30 * time.Second, 60 * time.Second, 120 * time.Second,
	240 * time.Second, 300 * time.Second, 300 * time.Second,
}

func newSlowPollTracker(t *testing.T) (*WorkspaceTracker, string) {
	t.Helper()
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)
	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	t.Cleanup(wt.Stop)
	wt.SetPollMode(PollModeSlow)
	return wt, repoDir
}

func slowIntervals(wt *WorkspaceTracker) (filePoll, gitPoll time.Duration) {
	filePoll, gitPoll, _ = wt.pollIntervals(PollModeSlow)
	return filePoll, gitPoll
}

// runUnchangedMonitorTicks performs n monitor scans that observe no change.
func runUnchangedMonitorTicks(t *testing.T, wt *WorkspaceTracker, last *workspaceState, n int) {
	t.Helper()
	var failures int
	for i := 0; i < n; i++ {
		if stop := wt.monitorTick(context.Background(), last, &failures); stop || failures != 0 {
			t.Fatalf("monitor tick %d: stop=%v failures=%d", i+1, stop, failures)
		}
	}
}

// primeGitPoll runs one Git poll scan so the cached HEAD, branch, and index
// match the repository; later scans then observe no change.
func primeGitPoll(t *testing.T, wt *WorkspaceTracker) {
	t.Helper()
	var failures int
	if stop := wt.gitPollTick(context.Background(), &failures); stop || failures != 0 {
		t.Fatalf("priming git poll tick: stop=%v failures=%d", stop, failures)
	}
}

func TestSlowPollMonitorBacksOffWhileScansAreUnchanged(t *testing.T) {
	wt, repoDir := newSlowPollTracker(t)
	last, err := wt.getWorkspaceState(context.Background())
	if err != nil {
		t.Fatalf("getWorkspaceState() error = %v", err)
	}

	for i, want := range slowBackoffSchedule {
		runUnchangedMonitorTicks(t, wt, &last, 1)
		filePoll, gitPoll := slowIntervals(wt)
		if filePoll != want {
			t.Fatalf("after %d unchanged monitor scans: file interval = %v, want %v", i+1, filePoll, want)
		}
		if gitPoll != defaultSlowPollInterval {
			t.Fatalf("monitor scans changed the git interval to %v", gitPoll)
		}
	}

	if err := os.WriteFile(filepath.Join(repoDir, "changed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write change: %v", err)
	}
	var failures int
	wt.monitorTick(context.Background(), &last, &failures)
	if filePoll, _ := slowIntervals(wt); filePoll != defaultSlowPollInterval {
		t.Fatalf("after a changed monitor scan: file interval = %v, want %v", filePoll, defaultSlowPollInterval)
	}
}

func TestSlowPollGitLoopBacksOffOnItsOwnCounter(t *testing.T) {
	wt, repoDir := newSlowPollTracker(t)
	primeGitPoll(t, wt)

	for i, want := range slowBackoffSchedule {
		primeGitPoll(t, wt)
		filePoll, gitPoll := slowIntervals(wt)
		if gitPoll != want {
			t.Fatalf("after %d unchanged git scans: git interval = %v, want %v", i+1, gitPoll, want)
		}
		if filePoll != defaultSlowPollInterval {
			t.Fatalf("git scans changed the file interval to %v", filePoll)
		}
	}

	writeFile(t, repoDir, "commit.txt", "x")
	runGit(t, repoDir, "add", "commit.txt")
	runGit(t, repoDir, "commit", "-m", "Move HEAD")
	primeGitPoll(t, wt)
	if _, gitPoll := slowIntervals(wt); gitPoll != defaultSlowPollInterval {
		t.Fatalf("after HEAD moved: git interval = %v, want %v", gitPoll, defaultSlowPollInterval)
	}
}

func TestSlowPollFailedScanDoesNotAdvanceBackoff(t *testing.T) {
	wt, _ := newSlowPollTracker(t)
	last, err := wt.getWorkspaceState(context.Background())
	if err != nil {
		t.Fatalf("getWorkspaceState() error = %v", err)
	}
	runUnchangedMonitorTicks(t, wt, &last, 3)
	if filePoll, _ := slowIntervals(wt); filePoll != 60*time.Second {
		t.Fatalf("file interval = %v, want 60s before the failed scan", filePoll)
	}

	wt.SetGitEnvironment([]string{"GIT_DIR=" + filepath.Join(t.TempDir(), "missing")})
	var failures int
	if stop := wt.monitorTick(context.Background(), &last, &failures); stop || failures != 1 {
		t.Fatalf("failing monitor tick: stop=%v failures=%d, want one failure", stop, failures)
	}
	if filePoll, _ := slowIntervals(wt); filePoll != 60*time.Second {
		t.Fatalf("file interval after a failed scan = %v, want 60s", filePoll)
	}
}

func TestSlowPollModePushRestoresBaseIntervalAndWakesLoops(t *testing.T) {
	wt, _ := newSlowPollTracker(t)
	last, err := wt.getWorkspaceState(context.Background())
	if err != nil {
		t.Fatalf("getWorkspaceState() error = %v", err)
	}
	primeGitPoll(t, wt)
	runUnchangedMonitorTicks(t, wt, &last, 4)
	for i := 0; i < 4; i++ {
		primeGitPoll(t, wt)
	}
	drainModeSignals(wt)

	// A push that repeats slow mode is the gateway reporting renewed interest.
	wt.SetPollMode(PollModeSlow)
	if filePoll, gitPoll := slowIntervals(wt); filePoll != defaultSlowPollInterval || gitPoll != defaultSlowPollInterval {
		t.Fatalf("after a same-mode push: intervals = %v/%v, want %v", filePoll, gitPoll, defaultSlowPollInterval)
	}
	for name, ch := range map[string]chan struct{}{"monitor": wt.monitorModeChanged, "gitPoll": wt.gitPollModeChanged} {
		select {
		case <-ch:
		default:
			t.Fatalf("%s loop was not woken to re-arm its lengthened timer", name)
		}
	}

	// Nothing was lengthened now, so another same-mode push stays a no-op.
	wt.SetPollMode(PollModeSlow)
	for name, ch := range map[string]chan struct{}{"monitor": wt.monitorModeChanged, "gitPoll": wt.gitPollModeChanged} {
		select {
		case <-ch:
			t.Fatalf("%s loop was woken although no interval was lengthened", name)
		default:
		}
	}

	// A mode transition also returns slow mode to its base interval.
	runUnchangedMonitorTicks(t, wt, &last, 4)
	wt.SetPollMode(PollModeFast)
	wt.SetPollMode(PollModeSlow)
	if filePoll, _ := slowIntervals(wt); filePoll != defaultSlowPollInterval {
		t.Fatalf("after fast and back to slow: file interval = %v, want %v", filePoll, defaultSlowPollInterval)
	}
}

func TestSlowPollBackoffLeavesFastAndPausedIntervals(t *testing.T) {
	wt, _ := newSlowPollTracker(t)
	last, err := wt.getWorkspaceState(context.Background())
	if err != nil {
		t.Fatalf("getWorkspaceState() error = %v", err)
	}
	primeGitPoll(t, wt)
	runUnchangedMonitorTicks(t, wt, &last, 6)
	for i := 0; i < 6; i++ {
		primeGitPoll(t, wt)
	}

	if file, git, paused := wt.pollIntervals(PollModeFast); file != wt.filePollInterval || git != wt.gitPollInterval || paused {
		t.Fatalf("fast intervals = %v/%v paused=%v, want %v/%v", file, git, paused, wt.filePollInterval, wt.gitPollInterval)
	}
	if file, git, paused := wt.pollIntervals(PollModePaused); file != pausedTickInterval || git != pausedTickInterval || !paused {
		t.Fatalf("paused intervals = %v/%v paused=%v, want %v paused", file, git, paused, pausedTickInterval)
	}
}

// TestSlowPollRunningLoopsConsumeBackoffResetWake complements the direct wake
// check above: with both loops running in slow mode and lengthened intervals,
// a same-mode push is received by each loop, which re-arms from pollIntervals
// at the base interval without running a scan.
func TestSlowPollRunningLoopsConsumeBackoffResetWake(t *testing.T) {
	wt, _ := newSlowPollTracker(t)
	wt.Start(context.Background())
	<-wt.initialScanDone
	completedBefore, _ := wt.MonitorTickStats()

	wt.slowIdleMonitorTicks.Store(6)
	wt.slowIdleGitPollTicks.Store(6)
	wt.SetPollMode(PollModeSlow)

	deadline := time.Now().Add(5 * time.Second)
	for len(wt.monitorModeChanged) != 0 || len(wt.gitPollModeChanged) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("running loops did not receive the wake: monitor=%d gitPoll=%d pending",
				len(wt.monitorModeChanged), len(wt.gitPollModeChanged))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if filePoll, gitPoll := slowIntervals(wt); filePoll != defaultSlowPollInterval || gitPoll != defaultSlowPollInterval {
		t.Fatalf("re-armed intervals = %v/%v, want %v", filePoll, gitPoll, defaultSlowPollInterval)
	}
	if completedAfter, _ := wt.MonitorTickStats(); completedAfter != completedBefore {
		t.Fatalf("slow wake ran %d monitor scans, want none", completedAfter-completedBefore)
	}
}

func drainModeSignals(wt *WorkspaceTracker) {
	for _, ch := range []chan struct{}{wt.monitorModeChanged, wt.gitPollModeChanged} {
		select {
		case <-ch:
		default:
		}
	}
}
