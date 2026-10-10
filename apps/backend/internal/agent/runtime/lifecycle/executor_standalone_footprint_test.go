package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
)

// TestStandaloneReadRuntimeFootprintsUsesRuntimeOwner pins that the footprint
// read resolves its control binding through the runtime owner when the executor
// was constructed without a static control client (the production shape).
// Reading the raw static field left it nil in production and panicked the
// backend on the maintenance tick.
func TestStandaloneReadRuntimeFootprintsUsesRuntimeOwner(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/runtime-footprint" {
			http.NotFound(w, r)
			return
		}
		requests++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"footprints": []map[string]any{{
				"instance_id": "instance-1",
				"session_id":  "session-1",
				"processes":   3,
			}},
		})
	}))
	defer server.Close()
	host, port := splitTestServerHostPort(t, server)

	owner := agentctl.NewRuntimeOwner(nil, newTestLogger(), "boot-one")
	t.Cleanup(owner.Stop)
	binding, err := owner.PrepareBinding()
	if err != nil {
		t.Fatalf("prepare binding: %v", err)
	}
	if err := binding.Configure(host, port, "secret", 11, nil); err != nil {
		t.Fatalf("configure binding: %v", err)
	}
	if err := binding.Commit(); err != nil {
		t.Fatalf("commit binding: %v", err)
	}

	executor := NewStandaloneExecutor(nil, "", 0, newTestLogger())
	executor.SetRuntimeOwner(owner)

	footprints, err := executor.ReadRuntimeFootprints(context.Background())
	if err != nil {
		t.Fatalf("ReadRuntimeFootprints: %v", err)
	}
	if requests != 1 {
		t.Fatalf("control requests = %d, want 1", requests)
	}
	if len(footprints) != 1 || footprints[0].InstanceID != "instance-1" {
		t.Fatalf("footprints = %+v, want the instance-1 reading", footprints)
	}
}

// TestStandaloneReadRuntimeFootprintsUnavailableWithoutControl pins the
// fail-closed direction: an executor with neither a static client nor a runtime
// owner returns the unavailable error instead of dereferencing a nil client.
func TestStandaloneReadRuntimeFootprintsUnavailableWithoutControl(t *testing.T) {
	executor := NewStandaloneExecutor(nil, "", 0, newTestLogger())
	if _, err := executor.ReadRuntimeFootprints(context.Background()); !errors.Is(err, agentctl.ErrRuntimeUnavailable) {
		t.Fatalf("ReadRuntimeFootprints error = %v, want ErrRuntimeUnavailable", err)
	}
}
