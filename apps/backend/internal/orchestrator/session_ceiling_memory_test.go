package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/kandev/kandev/pkg/api/v1"

	"github.com/kandev/kandev/internal/system/hostmemory"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// fakeMemoryReader is a deterministic host-memory source for the ceiling tests.
type fakeMemoryReader struct {
	reading hostmemory.Reading
	err     error
	calls   int
}

func (f *fakeMemoryReader) Read(context.Context) (hostmemory.Reading, error) {
	f.calls++
	return f.reading, f.err
}

func testMemoryGate() hostmemory.Gate {
	return hostmemory.Gate{Enabled: true, MinAvailableBytes: 8 << 30, MaxCommitPercent: 75}
}

func pressuredReading() hostmemory.Reading {
	return hostmemory.Reading{
		Complete: true, TotalBytes: 64 << 30, AvailableBytes: 2 << 30,
		CommitUsedBytes: 50 << 30, CommitLimitBytes: 100 << 30, CommitKnown: true,
	}
}

func clearReading() hostmemory.Reading {
	return hostmemory.Reading{
		Complete: true, TotalBytes: 64 << 30, AvailableBytes: 40 << 30,
		CommitUsedBytes: 10 << 30, CommitLimitBytes: 100 << 30, CommitKnown: true,
	}
}

func newMemoryController(ceiling int, lister *fakeAdmittedLister, reader *fakeMemoryReader) *sessionCeilingController {
	c := newSessionCeilingController(ceiling, lister, zap.NewNop())
	c.configureMemoryGate(testMemoryGate(), reader, nil)
	return c
}

func memoryRequest(sessionID string, origin launchOrigin) admissionRequest {
	return admissionRequest{taskID: "task-1", sessionID: sessionID, origin: origin, seam: "test"}
}

func requireMemoryRefusal(t *testing.T, c *sessionCeilingController, decision admissionDecision) {
	t.Helper()
	if decision.admitted {
		t.Fatal("launch was admitted while the host was pressured")
	}
	if decision.reasonCode != ceilingReasonMemoryPressure {
		t.Fatalf("reasonCode = %q, want %q", decision.reasonCode, ceilingReasonMemoryPressure)
	}
	if decision.populationKnown {
		t.Fatal("a memory refusal must not carry a population reading")
	}
	if len(c.reservations) != 0 {
		t.Fatalf("a memory refusal must not reserve capacity, got %d reservations", len(c.reservations))
	}
}

func TestSessionCeilingMemoryDefersAutomaticLaunch(t *testing.T) {
	c := newMemoryController(10, &fakeAdmittedLister{}, &fakeMemoryReader{reading: pressuredReading()})
	requireMemoryRefusal(t, c, c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic)))
}

func TestSessionCeilingMemoryHoldsLaunchWithUnlimitedCeiling(t *testing.T) {
	c := newMemoryController(unlimitedSessionCeiling, &fakeAdmittedLister{}, &fakeMemoryReader{reading: pressuredReading()})
	requireMemoryRefusal(t, c, c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic)))
}

func TestSessionCeilingMemoryAdmitsWhenClear(t *testing.T) {
	c := newMemoryController(10, &fakeAdmittedLister{}, &fakeMemoryReader{reading: clearReading()})
	decision := c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("launch was not admitted on a clear host: reason=%q", decision.reasonCode)
	}
}

func TestSessionCeilingMemoryCommitAboveCeilingDefers(t *testing.T) {
	reading := clearReading()
	reading.CommitUsedBytes = 80 << 30 // 80% of a 100 GiB limit
	c := newMemoryController(10, &fakeAdmittedLister{}, &fakeMemoryReader{reading: reading})
	requireMemoryRefusal(t, c, c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic)))
}

func TestSessionCeilingMemoryManualLaunchIsDeferred(t *testing.T) {
	c := newMemoryController(10, &fakeAdmittedLister{}, &fakeMemoryReader{reading: pressuredReading()})
	requireMemoryRefusal(t, c, c.admit(context.Background(), memoryRequest("s-manual", launchOriginManual)))
}

// The manual override still passes the session-count ceiling on a clear host;
// only the host-memory condition is closed to it.
func TestSessionCeilingMemoryManualOverridesCountCeilingWhenClear(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("running-1")
	c := newMemoryController(1, lister, &fakeMemoryReader{reading: clearReading()})
	decision := c.admit(context.Background(), memoryRequest("s-manual", launchOriginManual))
	if !decision.admitted || !decision.manualOverride {
		t.Fatalf("manual launch must keep its count-ceiling override: admitted=%v override=%v",
			decision.admitted, decision.manualOverride)
	}
}

// A message or resume into a session that is already counted adds no process
// and is never held, for either origin.
func TestSessionCeilingMemoryRunningSessionIsNotHeld(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("s-counted")
	c := newMemoryController(10, lister, &fakeMemoryReader{reading: pressuredReading()})
	for _, origin := range []launchOrigin{launchOriginAutomatic, launchOriginManual} {
		decision := c.admit(context.Background(), memoryRequest("s-counted", origin))
		if !decision.admitted {
			t.Fatalf("a running session (%s) must not be held: reason=%q", origin, decision.reasonCode)
		}
	}
}

func TestSessionCeilingMemoryHandOffIsNotRefused(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("s-counted")
	c := newMemoryController(10, lister, &fakeMemoryReader{reading: pressuredReading()})
	decision := c.handOffOrAdmit(context.Background(), memoryRequest("s-counted", launchOriginAutomatic))
	if !decision.admitted || !decision.handedOff {
		t.Fatalf("hand-off under pressure: admitted=%v handedOff=%v reason=%q",
			decision.admitted, decision.handedOff, decision.reasonCode)
	}
}

func TestSessionCeilingMemoryHandOffAdmissionOfNewSessionIsHeld(t *testing.T) {
	c := newMemoryController(10, &fakeAdmittedLister{}, &fakeMemoryReader{reading: pressuredReading()})
	requireMemoryRefusal(t, c, c.handOffOrAdmit(context.Background(), memoryRequest("s-new", launchOriginAutomatic)))
}

func TestSessionCeilingMemoryExemptTaskIsAdmitted(t *testing.T) {
	reader := &fakeMemoryReader{reading: pressuredReading()}
	c := newSessionCeilingController(10, &fakeAdmittedLister{}, zap.NewNop())
	c.configureMemoryGate(testMemoryGate(), reader, func(_ context.Context, taskID string) bool {
		return taskID == "task-1"
	})
	decision := c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("exempt task was held by host memory: reason=%q", decision.reasonCode)
	}
	if reader.calls != 0 {
		t.Fatalf("an exempt launch must not read the host, got %d reads", reader.calls)
	}
}

func TestSessionCeilingMemoryFailsOpen(t *testing.T) {
	for name, reader := range map[string]*fakeMemoryReader{
		"reader error":       {err: errors.New("boom")},
		"incomplete reading": {reading: hostmemory.Reading{Complete: false}},
	} {
		t.Run(name, func(t *testing.T) {
			c := newMemoryController(10, &fakeAdmittedLister{}, reader)
			decision := c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic))
			if !decision.admitted {
				t.Fatalf("an unmeasurable host must fail open: reason=%q", decision.reasonCode)
			}
		})
	}
}

func TestSessionCeilingMemoryDisabledGateNeverReadsHost(t *testing.T) {
	reader := &fakeMemoryReader{reading: pressuredReading()}
	c := newSessionCeilingController(10, &fakeAdmittedLister{}, zap.NewNop())
	c.configureMemoryGate(hostmemory.Gate{}, reader, nil)
	decision := c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("a disabled gate must not defer: reason=%q", decision.reasonCode)
	}
	if reader.calls != 0 {
		t.Fatalf("a disabled gate must not read the host, got %d reads", reader.calls)
	}
}

func TestSessionCeilingMemoryWarningLoggedOncePerStreak(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	c := newSessionCeilingController(10, &fakeAdmittedLister{}, zap.New(core))
	reader := &fakeMemoryReader{err: errors.New("boom")}
	c.configureMemoryGate(testMemoryGate(), reader, nil)
	for i := 0; i < 3; i++ {
		if decision := c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic)); !decision.admitted {
			t.Fatalf("fail-open admission %d failed", i)
		}
	}
	if n := logs.FilterMessageSnippet("failing open").Len(); n != 1 {
		t.Fatalf("the memory-read failure warning must be logged once, got %d", n)
	}
}

func TestSessionCeilingMemoryReentryAfterPressureClears(t *testing.T) {
	reader := &fakeMemoryReader{reading: pressuredReading()}
	c := newMemoryController(10, &fakeAdmittedLister{}, reader)
	if decision := c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic)); decision.admitted {
		t.Fatal("launch was admitted while pressured")
	}
	reader.reading = clearReading()
	if decision := c.admit(context.Background(), memoryRequest("s-new", launchOriginAutomatic)); !decision.admitted {
		t.Fatalf("launch was not admitted after the pressure cleared: reason=%q", decision.reasonCode)
	}
}

func TestCoordinatorTaskMemoryExemption(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	if err := repo.CreateWorkspace(ctx, &models.Workspace{ID: "ws-memory", Name: "Memory"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	now := time.Now().UTC()
	for _, task := range []*models.Task{
		{ID: "task-coordinator", WorkspaceID: "ws-memory", Title: "Coordinator", Origin: models.TaskOriginCoordinator,
			State: v1.TaskStateInProgress, CreatedAt: now, UpdatedAt: now},
		{ID: "task-worker", WorkspaceID: "ws-memory", Title: "Worker",
			State: v1.TaskStateInProgress, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.CreateTask(ctx, task); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}
	svc := &Service{repo: repo}
	if !svc.coordinatorTaskMemoryExemption(ctx, "task-coordinator") {
		t.Fatal("coordinator task must be exempt")
	}
	if svc.coordinatorTaskMemoryExemption(ctx, "task-worker") {
		t.Fatal("ordinary task must not be exempt")
	}
	if svc.coordinatorTaskMemoryExemption(ctx, "task-missing") {
		t.Fatal("unreadable task must not be exempt")
	}
}
