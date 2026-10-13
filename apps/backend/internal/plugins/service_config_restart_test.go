package plugins

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRestartForConfigChangeRefreshesBothNotificationsOnFailure(t *testing.T) {
	svc, _, fsStore, rt := newTestServiceWithDir(t)
	installTestPlugin(t, svc, "kandev-plugin-slack")

	// Wire start failure through the service's existing fakeRuntime.
	rt.setStartErr("kandev-plugin-slack", errors.New("restart handshake failed"))

	deliverer := &fakeDeliverer{}
	svc.SetDeliverer(deliverer)
	catalogListener := &countingAgentToolCatalogListener{}
	svc.SetAgentToolCatalogListener(catalogListener)

	// Restart failure: UpdateConfig should fail, but both notifications must fire.
	err := svc.UpdateConfig(context.Background(), "kandev-plugin-slack", map[string]any{
		"new_key": "new_value",
	})
	if err == nil {
		t.Fatal("UpdateConfig expected error due to restart failure")
	}

	if deliverer.refreshCount != 1 {
		t.Errorf("deliverer.Refresh() = %d, want 1", deliverer.refreshCount)
	}
	if catalogListener.calls.Load() != 2 {
		// UpdateConfig calls notifyAgentToolCatalogChanged once before the
		// restart; restart failure calls it a second time.
		t.Errorf("catalog NotifyAgentToolCatalogChanged() = %d, want 2", catalogListener.calls.Load())
	}

	// Confirm status moved to StatusError (setStatusAndDiagnostic succeeds in this path).
	got, getErr := svc.Get("kandev-plugin-slack")
	if getErr != nil {
		t.Fatalf("Get() after restart failure: %v", getErr)
	}
	if got.Status != StatusError {
		t.Errorf("status = %q, want %q", got.Status, StatusError)
	}

	// Verify the diagnostic reflects the restart error.
	if got.LastError == "" || !strings.Contains(got.LastError, "restart handshake failed") {
		t.Errorf("LastError = %q, want restart error text", got.LastError)
	}

	// Confirm persistence: FSStore.Get returns (*store.Record, error).
	onDisk, fsErr := fsStore.Get("kandev-plugin-slack")
	if fsErr != nil {
		t.Fatalf("fsStore.Get() after restart failure: %v", fsErr)
	}
	if onDisk.Status != StatusError {
		t.Errorf("persisted status = %q, want %q", onDisk.Status, StatusError)
	}
}
