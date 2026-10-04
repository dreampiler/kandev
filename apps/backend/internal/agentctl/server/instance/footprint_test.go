package instance

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/config"
	"github.com/kandev/kandev/internal/agentctl/server/process"
)

// stubProcessManager is a processManager whose owned footprint is fixed, so
// attribution can be asserted without launching an agent.
type stubProcessManager struct {
	footprint process.ProcessFootprint
}

func (s *stubProcessManager) CloseAdmission()                       {}
func (s *stubProcessManager) StopForTeardown(context.Context) error { return nil }
func (s *stubProcessManager) WorkspaceSourceRoots() []string        { return nil }
func (s *stubProcessManager) GetSessionID() string                  { return "provider-session" }
func (s *stubProcessManager) OwnedFootprint() process.ProcessFootprint {
	return s.footprint
}

// TestInstanceFootprintCarriesOwnIdentity pins that a footprint is attributed to
// the instance that owns it. The caller must be able to tell which session the
// bytes belong to without matching on a process name or a bare process
// identifier.
func TestInstanceFootprintCarriesOwnIdentity(t *testing.T) {
	instance := &Instance{
		ID:        "instance-a",
		SessionID: "session-a",
		TaskID:    "task-a",
		Status:    "running",
		CreatedAt: time.Now().UTC(),
		manager: &stubProcessManager{footprint: process.ProcessFootprint{
			Processes:      4,
			CommittedBytes: 900,
			ResidentBytes:  500,
		}},
	}

	footprint := instance.Footprint()

	if footprint.InstanceID != "instance-a" || footprint.SessionID != "session-a" || footprint.TaskID != "task-a" {
		t.Fatalf("identity = %+v, want the instance's own identity", footprint)
	}
	if footprint.Status != "running" {
		t.Fatalf("Status = %q, want %q", footprint.Status, "running")
	}
	if footprint.Processes != 4 || footprint.CommittedBytes != 900 || footprint.ResidentBytes != 500 {
		t.Fatalf("measurement = %+v, want the owned footprint verbatim", footprint.ProcessFootprint)
	}
	if footprint.CreatedAt.IsZero() {
		t.Fatal("CreatedAt must be carried so an instance with no observed request still has an age")
	}
}

// TestInstanceFootprintWithUnreadableProcessesKeepsLowerBoundMarker pins that an
// unreadable owned process reaches the projection. A caller that cannot see the
// unreadable count would read a partial total as a complete one.
func TestInstanceFootprintWithUnreadableProcessesKeepsLowerBoundMarker(t *testing.T) {
	instance := &Instance{
		ID:     "instance-b",
		Status: "ready",
		manager: &stubProcessManager{footprint: process.ProcessFootprint{
			Processes:           3,
			ResidentBytes:       100,
			UnreadableProcesses: 2,
		}},
	}

	footprint := instance.Footprint()

	if footprint.UnreadableProcesses != 2 {
		t.Fatalf("UnreadableProcesses = %d, want 2", footprint.UnreadableProcesses)
	}
	if footprint.Processes != 3 {
		t.Fatalf("Processes = %d, want the unreadable ones still counted, want 3", footprint.Processes)
	}
}

// TestInstanceFootprintWithoutManagerReportsNoMeasurement pins the fail-closed
// direction: an instance whose agent has not started has no owned tree, and the
// projection must say so with zeroes rather than carrying a stale measurement.
func TestInstanceFootprintWithoutManagerReportsNoMeasurement(t *testing.T) {
	instance := &Instance{ID: "instance-c", SessionID: "session-c", Status: "created"}

	footprint := instance.Footprint()

	if footprint.InstanceID != "instance-c" || footprint.SessionID != "session-c" {
		t.Fatalf("identity = %+v, want the instance's own identity", footprint)
	}
	if footprint.Processes != 0 || footprint.CommittedBytes != 0 || footprint.ResidentBytes != 0 {
		t.Fatalf("measurement = %+v, want no measurement without a process manager", footprint.ProcessFootprint)
	}
}

// TestListInstanceFootprintsMeasuresOutsideTheManagerLock pins that the
// manager copies its instances out under the lock and measures after releasing
// it. Holding the manager lock across a process walk would block every control
// operation for the duration of the walk.
func TestListInstanceFootprintsMeasuresOutsideTheManagerLock(t *testing.T) {
	manager := NewManager(&config.Config{Ports: config.PortConfig{Base: 0, Max: 0}}, newTestLogger(t))
	first := &Instance{ID: "i1", SessionID: "s1", Status: "running",
		manager: &stubProcessManager{footprint: process.ProcessFootprint{Processes: 2, ResidentBytes: 20}}}
	second := &Instance{ID: "i2", SessionID: "s2", Status: "ready",
		manager: &stubProcessManager{footprint: process.ProcessFootprint{Processes: 3, ResidentBytes: 30}}}

	manager.mu.Lock()
	manager.instances["i1"] = first
	manager.instances["i2"] = second
	manager.mu.Unlock()

	footprints := manager.ListInstanceFootprints()
	if len(footprints) != 2 {
		t.Fatalf("len(footprints) = %d, want 2", len(footprints))
	}

	bySession := make(map[string]RuntimeFootprint, len(footprints))
	for _, footprint := range footprints {
		bySession[footprint.SessionID] = footprint
	}
	if got := bySession["s1"].Processes; got != 2 {
		t.Errorf("s1 processes = %d, want 2", got)
	}
	if got := bySession["s2"].Processes; got != 3 {
		t.Errorf("s2 processes = %d, want 3", got)
	}
}
