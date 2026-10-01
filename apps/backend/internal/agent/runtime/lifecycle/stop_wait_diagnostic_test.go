package lifecycle

import (
	"testing"
	"time"
)

func TestUpdateStreamStageStopWaitReportsBlockedPhaseOnce(t *testing.T) {
	observed := make(chan stopWaitPhase, 2)
	watch := newStopWaitWatch(time.Millisecond, func(phase stopWaitPhase) {
		observed <- phase
	})
	watch.advance(stopWaitActivity)
	select {
	case phase := <-observed:
		if phase != stopWaitActivity {
			t.Fatalf("blocked phase = %v, want activity", phase)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked stop did not report its wait phase")
	}
	watch.finish()
	select {
	case phase := <-observed:
		t.Fatalf("stop reported twice, second phase %v", phase)
	case <-time.After(10 * time.Millisecond):
	}
}

func TestUpdateStreamStageCompletedStopHasNoDiagnostic(t *testing.T) {
	observed := make(chan stopWaitPhase, 1)
	watch := newStopWaitWatch(time.Hour, func(phase stopWaitPhase) {
		observed <- phase
	})
	watch.finish()
	select {
	case phase := <-observed:
		t.Fatalf("completed stop reported blocked phase %v", phase)
	default:
	}
}
