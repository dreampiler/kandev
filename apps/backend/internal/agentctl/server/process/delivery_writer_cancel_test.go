package process

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/adapter"
)

// TestDeliveryWriterLifetimeCancelUnblocksReserve verifies the writer's own
// context is derived from the manager lifetime, so a reserve parked on queue
// space is released by lifecycle cancellation instead of hanging until close.
func TestDeliveryWriterLifetimeCancelUnblocksReserve(t *testing.T) {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	m := &Manager{logger: newTestLogger(t), lifetimeCtx: lifetimeCtx}
	writer := newDeliveryEventWriter(m)
	t.Cleanup(func() { writer.close() })

	writer.mu.Lock()
	writer.queued = deliveryWriterQueueBytes
	writer.mu.Unlock()

	reserved := make(chan error, 1)
	go func() { reserved <- writer.reserve(context.Background(), 1) }()

	lifetimeCancel()

	select {
	case err := <-reserved:
		if !errors.Is(err, errDeliveryWriterClosed) {
			t.Fatalf("reserve after lifetime cancel = %v, want errDeliveryWriterClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reserve stayed parked after lifetime cancellation")
	}
}

// TestDeliveryWriterCloseUnblocksReserveAndStopsRun verifies the shutdown entry
// releases a parked reserve and lets run drain to completion.
func TestDeliveryWriterCloseUnblocksReserveAndStopsRun(t *testing.T) {
	m := &Manager{logger: newTestLogger(t)}
	writer := newDeliveryEventWriter(m)

	writer.mu.Lock()
	writer.queued = deliveryWriterQueueBytes
	writer.mu.Unlock()

	reserved := make(chan error, 1)
	go func() { reserved <- writer.reserve(context.Background(), 1) }()

	closed := make(chan struct{})
	go func() {
		writer.close()
		close(closed)
	}()

	select {
	case err := <-reserved:
		if !errors.Is(err, errDeliveryWriterClosed) {
			t.Fatalf("reserve after close = %v, want errDeliveryWriterClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reserve stayed parked after writer close")
	}

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("writer close did not complete run shutdown")
	}
}

// TestDeliveryWriterPersistsWithoutJournal verifies the normal commit path is
// unchanged when no durable journal is configured.
func TestDeliveryWriterPersistsWithoutJournal(t *testing.T) {
	m := &Manager{logger: newTestLogger(t)}
	writer := newDeliveryEventWriter(m)
	t.Cleanup(func() { writer.close() })

	got, err := writer.persist(context.Background(), adapter.AgentEvent{Type: adapter.EventTypeMessageChunk})
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if got.Type != adapter.EventTypeMessageChunk {
		t.Fatalf("persist returned %+v, want the original event", got)
	}
}
