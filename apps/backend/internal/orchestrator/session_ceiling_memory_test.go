package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/system/hostmemory"
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
	return hostmemory.Gate{Enabled: true, MinAvailableBytes: 8 << 30, MaxCommitPercent: 85}
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

func newMemoryController(t *testing.T, ceiling int, lister *fakeAdmittedLister, reader *fakeMemoryReader) *sessionCeilingController {
	t.Helper()
	c := newSessionCeilingController(ceiling, lister, zap.NewNop())
	c.memoryGate = testMemoryGate()
	c.memoryReader = reader
	return c
}

func workerRequest(sessionID string, origin launchOrigin) admissionRequest {
	return admissionRequest{taskID: "task-1", sessionID: sessionID, origin: origin, seam: "test", agentProfileID: ""}
}

func TestSessionCeilingMemoryDefersAutomaticWorkerLaunch(t *testing.T) {
	lister := &fakeAdmittedLister{}
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: pressuredReading()})

	decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic))
	if decision.admitted {
		t.Fatal("automatic worker launch was admitted while the host was pressured")
	}
	if decision.reasonCode != ceilingReasonMemoryPressure {
		t.Fatalf("reasonCode = %q, want %q", decision.reasonCode, ceilingReasonMemoryPressure)
	}
	if len(c.reservations) != 0 {
		t.Fatalf("a memory refusal must not reserve capacity, got %d reservations", len(c.reservations))
	}
}

func TestSessionCeilingMemoryAdmitsWhenClear(t *testing.T) {
	lister := &fakeAdmittedLister{}
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: clearReading()})

	decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("automatic worker launch was not admitted on a clear host: reason=%q", decision.reasonCode)
	}
}

func TestSessionCeilingMemoryCommitAboveCeilingDefers(t *testing.T) {
	reading := clearReading()
	reading.CommitUsedBytes = 95 << 30 // 95% of a 100 GiB limit
	lister := &fakeAdmittedLister{}
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: reading})

	decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic))
	if decision.admitted || decision.reasonCode != ceilingReasonMemoryPressure {
		t.Fatalf("commit above ceiling: admitted=%v reason=%q", decision.admitted, decision.reasonCode)
	}
}

func TestSessionCeilingMemoryManualOverrides(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("worker-1") // saturate the worker lane so the override branch is reached
	c := newMemoryController(t, 1, lister, &fakeMemoryReader{reading: pressuredReading()})

	decision := c.admit(context.Background(), workerRequest("s-manual", launchOriginManual))
	if !decision.admitted || !decision.manualOverride {
		t.Fatalf("manual launch must keep its override under pressure: admitted=%v override=%v", decision.admitted, decision.manualOverride)
	}
}

func TestSessionCeilingMemoryControlLaneIsExempt(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("worker-1")
	c := newMemoryController(t, 1, lister, &fakeMemoryReader{reading: pressuredReading()})
	c.controlCeiling = 2
	c.controlProfiles = controlProfileSet([]string{"ctrl"})

	req := workerRequest("ctrl-new", launchOriginAutomatic)
	req.agentProfileID = "ctrl"
	decision := c.admit(context.Background(), req)
	if !decision.admitted {
		t.Fatalf("control-lane launch was deferred by host memory: reason=%q", decision.reasonCode)
	}
	if decision.class != ceilingClassControl {
		t.Fatalf("class = %q, want control", decision.class)
	}
}

func TestSessionCeilingMemoryAlreadyAdmittedPasses(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("s-counted")
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: pressuredReading()})

	decision := c.admit(context.Background(), workerRequest("s-counted", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("an already-counted session must not be refused by memory: reason=%q", decision.reasonCode)
	}
}

func TestSessionCeilingMemoryHandOffIsNotRefused(t *testing.T) {
	lister := &fakeAdmittedLister{}
	lister.set("s-counted")
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: pressuredReading()})

	decision := c.handOffOrAdmit(context.Background(), workerRequest("s-counted", launchOriginAutomatic))
	if !decision.admitted || !decision.handedOff {
		t.Fatalf("hand-off under pressure: admitted=%v handedOff=%v reason=%q", decision.admitted, decision.handedOff, decision.reasonCode)
	}
}

func TestSessionCeilingMemoryFailsOpenOnReaderError(t *testing.T) {
	lister := &fakeAdmittedLister{}
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{err: errors.New("boom")})

	decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("a reader error must fail open: reason=%q", decision.reasonCode)
	}
}

func TestSessionCeilingMemoryFailsOpenOnIncompleteReading(t *testing.T) {
	lister := &fakeAdmittedLister{}
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: hostmemory.Reading{Complete: false}})

	decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("an incomplete reading must fail open: reason=%q", decision.reasonCode)
	}
}

func TestSessionCeilingMemoryDisabledGateNeverDefers(t *testing.T) {
	lister := &fakeAdmittedLister{}
	c := newSessionCeilingController(10, lister, zap.NewNop())
	c.memoryReader = &fakeMemoryReader{reading: pressuredReading()} // gate left disabled

	decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("a disabled gate must not defer: reason=%q", decision.reasonCode)
	}
}

func TestSessionCeilingMemoryWarningLoggedOnce(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	lister := &fakeAdmittedLister{}
	c := newSessionCeilingController(10, lister, zap.New(core))
	c.memoryGate = testMemoryGate()
	c.memoryReader = &fakeMemoryReader{err: errors.New("boom")}

	for i := 0; i < 3; i++ {
		if decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic)); !decision.admitted {
			t.Fatalf("fail-open admission %d failed", i)
		}
	}
	if n := logs.FilterMessageSnippet("failing open").Len(); n != 1 {
		t.Fatalf("the memory-read failure warning must be logged exactly once, got %d", n)
	}
}

func TestSessionCeilingMemorySweepArmReportsNoWorkerCapacity(t *testing.T) {
	lister := &fakeAdmittedLister{}
	c := newMemoryController(t, 10, lister, &fakeMemoryReader{reading: pressuredReading()})
	c.controlCeiling = 2
	c.controlProfiles = controlProfileSet([]string{"ctrl"})

	workerFree, controlFree, err := c.freeCapacityByLane(context.Background())
	if err != nil {
		t.Fatalf("freeCapacityByLane: %v", err)
	}
	if workerFree {
		t.Fatal("the worker lane must report no free capacity while the host is pressured")
	}
	if !controlFree {
		t.Fatal("memory pressure must not close the control lane")
	}
}

func TestSessionCeilingMemoryReentryAfterPressureClears(t *testing.T) {
	lister := &fakeAdmittedLister{}
	reader := &fakeMemoryReader{reading: pressuredReading()}
	c := newMemoryController(t, 10, lister, reader)

	if decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic)); decision.admitted {
		t.Fatal("launch was admitted while pressured")
	}
	reader.reading = clearReading()
	decision := c.admit(context.Background(), workerRequest("s-new", launchOriginAutomatic))
	if !decision.admitted {
		t.Fatalf("launch was not re-admitted after the pressure cleared: reason=%q", decision.reasonCode)
	}
}
