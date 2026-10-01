package process

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/adapter"
)

func TestUpdateStreamStageAdapterReceiptBeforeBlockedForward(t *testing.T) {
	stopCh := make(chan struct{})
	stub := newStubAdapter()
	m := &Manager{updatesCh: make(chan adapter.AgentEvent), logger: newTestLogger(t)}
	m.wg.Add(1)
	go m.forwardUpdates(stub, stopCh)
	defer func() { close(stopCh); m.wg.Wait() }()

	stub.updatesCh <- adapter.AgentEvent{Type: adapter.EventTypeMessageChunk}
	deadline := time.After(time.Second)
	for {
		stage := m.UpdateStreamStage()
		if stage.AdapterSeen == 1 {
			if stage.AdapterAgeMillis < 0 {
				t.Fatalf("adapter event age = %d, want available", stage.AdapterAgeMillis)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("adapter receipt count = %d, want 1 while downstream send is blocked", stage.AdapterSeen)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
