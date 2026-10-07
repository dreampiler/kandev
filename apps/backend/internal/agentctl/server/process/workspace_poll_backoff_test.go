package process

import (
	"testing"
	"time"
)

func TestSlowPollInterval_GrowsWithIdleTicks(t *testing.T) {
	base := 30 * time.Second
	cases := []struct {
		idleTicks uint32
		want      time.Duration
	}{
		{0, base},
		{1, base},
		{2, base},
		{3, base * 2},
		{4, base * 4},
		{5, base * 8},
		{6, base * 10},
		{10, base * 10},
		{100, base * 10},
	}
	for _, tc := range cases {
		got := slowPollInterval(base, tc.idleTicks)
		if got != tc.want {
			t.Errorf("slowPollInterval(%v, %d) = %v, want %v", base, tc.idleTicks, got, tc.want)
		}
	}
}

func TestNoteSlowPollIdle_ResetsOnChange(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.slowIdleMonitorTicks.Store(5)
	wt.noteSlowPollIdle(&wt.slowIdleMonitorTicks, true)
	if got := wt.slowIdleMonitorTicks.Load(); got != 0 {
		t.Errorf("after change, counter = %d, want 0", got)
	}
}

func TestNoteSlowPollIdle_IncrementsWhenUnchanged(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.slowIdleMonitorTicks.Store(2)
	wt.noteSlowPollIdle(&wt.slowIdleMonitorTicks, false)
	if got := wt.slowIdleMonitorTicks.Load(); got != 3 {
		t.Errorf("after unchanged scan, counter = %d, want 3", got)
	}
}

func TestResetSlowBackoff_ZeroesBothCounters(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.slowIdleMonitorTicks.Store(7)
	wt.slowIdleGitPollTicks.Store(9)

	wt.resetSlowBackoff()

	if got := wt.slowIdleMonitorTicks.Load(); got != 0 {
		t.Errorf("monitor counter = %d, want 0", got)
	}
	if got := wt.slowIdleGitPollTicks.Load(); got != 0 {
		t.Errorf("git poll counter = %d, want 0", got)
	}
}

func TestSetPollMode_ResetsBackoff(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.slowIdleMonitorTicks.Store(5)
	wt.slowIdleGitPollTicks.Store(5)

	wt.SetPollMode(PollModeSlow)

	if got := wt.slowIdleMonitorTicks.Load(); got != 0 {
		t.Errorf("monitor counter = %d, want 0", got)
	}
	if got := wt.slowIdleGitPollTicks.Load(); got != 0 {
		t.Errorf("git poll counter = %d, want 0", got)
	}
}

func TestGitPollIntervalForMode_FastIgnoresIdle(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.slowIdleGitPollTicks.Store(10)

	got := wt.gitPollIntervalForMode(3 * time.Second)
	if got != 3*time.Second {
		t.Errorf("fast mode interval = %v, want 3s", got)
	}
}

func TestGitPollIntervalForMode_SlowAppliesBackoff(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.SetPollMode(PollModeSlow)
	wt.slowIdleGitPollTicks.Store(4)

	got := wt.gitPollIntervalForMode(30 * time.Second)
	want := 30 * time.Second * 4
	if got != want {
		t.Errorf("slow mode interval = %v, want %v", got, want)
	}
}

func TestMonitorInterval_FastIgnoresIdle(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.slowIdleMonitorTicks.Store(10)

	got := wt.monitorInterval(2 * time.Second)
	if got != 2*time.Second {
		t.Errorf("fast mode interval = %v, want 2s", got)
	}
}

func TestMonitorInterval_SlowAppliesBackoff(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.SetPollMode(PollModeSlow)
	wt.slowIdleMonitorTicks.Store(3)

	got := wt.monitorInterval(30 * time.Second)
	want := 30 * time.Second * 2
	if got != want {
		t.Errorf("slow mode interval = %v, want %v", got, want)
	}
}

func TestMonitorInterval_PausedIgnoresIdle(t *testing.T) {
	isolateTestGitEnv(t)
	repoDir, cleanup := setupTestRepo(t)
	defer cleanup()

	wt := NewWorkspaceTracker(repoDir, newTestLogger(t))
	wt.SetPollMode(PollModePaused)
	wt.slowIdleMonitorTicks.Store(10)

	got := wt.monitorInterval(60 * time.Second)
	if got != 60*time.Second {
		t.Errorf("paused mode interval = %v, want 60s", got)
	}
}
