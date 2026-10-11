package process

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/adapter"
	"github.com/kandev/kandev/internal/agentctl/server/config"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

func permissionTimeoutTestManager(t *testing.T, updatesBuffer int) *Manager {
	return &Manager{
		cfg: &config.InstanceConfig{
			TaskID:    "task-1",
			SessionID: "session-1",
		},
		logger:             newTestLogger(t),
		updatesCh:          make(chan adapter.AgentEvent, updatesBuffer),
		pendingPermissions: make(map[string]*PendingPermission),
	}
}

func commandPermissionRequest(pendingID string) *adapter.PermissionRequest {
	return &adapter.PermissionRequest{
		PendingID:  pendingID,
		ToolCallID: "tool-1",
		Title:      "Run command",
		Options: []adapter.PermissionOption{
			{OptionID: "allow-once", Kind: streams.PermissionOptionKindAllowOnce},
		},
	}
}

// TestHandlePermissionRequestFailsOnResponseTimeout is the regression guard for
// a permission request that is never answered: the handler must fail it
// explicitly as cancelled and release the pending entry instead of waiting
// forever and stranding the agent.
func TestHandlePermissionRequestFailsOnResponseTimeout(t *testing.T) {
	restore := permissionResponseTimeout
	permissionResponseTimeout = 20 * time.Millisecond
	t.Cleanup(func() { permissionResponseTimeout = restore })

	m := permissionTimeoutTestManager(t, 4)

	resp, err := m.handlePermissionRequest(context.Background(), commandPermissionRequest("pending-1"))
	if err != nil {
		t.Fatalf("handlePermissionRequest: %v", err)
	}
	if resp == nil || !resp.Cancelled || resp.OptionID != "" {
		t.Fatalf("timed-out permission must fail closed as cancelled, got %+v", resp)
	}
	m.permissionMu.RLock()
	_, stillPending := m.pendingPermissions["pending-1"]
	m.permissionMu.RUnlock()
	if stillPending {
		t.Fatal("timed-out pending request was not settled and removed")
	}
}

// TestHandlePermissionRequestCancelledByLifetime verifies instance lifecycle
// cancellation ends the response wait immediately instead of waiting for the
// deadline.
func TestHandlePermissionRequestCancelledByLifetime(t *testing.T) {
	restore := permissionResponseTimeout
	permissionResponseTimeout = time.Hour
	t.Cleanup(func() { permissionResponseTimeout = restore })

	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	m := permissionTimeoutTestManager(t, 4)
	m.lifetimeCtx = lifetimeCtx

	done := make(chan *adapter.PermissionResponse, 1)
	go func() {
		resp, _ := m.handlePermissionRequest(context.Background(), commandPermissionRequest("pending-1"))
		done <- resp
	}()

	// The permission notification is published after the pending entry is
	// registered, so receiving it proves the wait is armed.
	<-m.updatesCh
	lifetimeCancel()

	select {
	case resp := <-done:
		if resp == nil || !resp.Cancelled {
			t.Fatalf("lifetime cancellation must fail the request as cancelled, got %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit on lifetime cancellation")
	}
}

// TestHandlePermissionRequestAcceptsResponseBeforeTimeout verifies a real
// response still wins over the finite deadline.
func TestHandlePermissionRequestAcceptsResponseBeforeTimeout(t *testing.T) {
	restore := permissionResponseTimeout
	permissionResponseTimeout = time.Hour
	t.Cleanup(func() { permissionResponseTimeout = restore })

	m := permissionTimeoutTestManager(t, 4)

	done := make(chan *adapter.PermissionResponse, 1)
	go func() {
		resp, _ := m.handlePermissionRequest(context.Background(), commandPermissionRequest("pending-1"))
		done <- resp
	}()

	event := <-m.updatesCh
	if _, err := m.ResolvePermission(event.RequestID, event.PendingID, "allow-once"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	select {
	case resp := <-done:
		if resp == nil || resp.Cancelled || resp.OptionID != "allow-once" {
			t.Fatalf("a delivered response must be returned verbatim, got %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return the delivered response")
	}
}

// TestSettlePermissionWaitDeliversConcurrentResponse verifies that when a
// concurrent resolve has already consumed the pending entry (so the settle
// cannot claim the transition), the response it delivered is returned rather
// than being replaced by a fabricated cancellation.
func TestSettlePermissionWaitDeliversConcurrentResponse(t *testing.T) {
	m := &Manager{
		logger:             newTestLogger(t),
		pendingPermissions: make(map[string]*PendingPermission),
	}
	responseCh := make(chan *adapter.PermissionResponse, 1)
	responseCh <- &adapter.PermissionResponse{OptionID: "allow-once"}
	pending := &PendingPermission{ID: "pending-1", RequestID: "request-1", ResponseCh: responseCh}

	got := m.settlePermissionWait(pending)
	if got == nil || got.Cancelled || got.OptionID != "allow-once" {
		t.Fatalf("settle must deliver the concurrent response, got %+v", got)
	}
}
