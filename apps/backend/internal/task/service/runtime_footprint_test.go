package service

import (
	"context"
	"testing"
	"time"

	agentruntime "github.com/kandev/kandev/internal/agentruntime"
	"github.com/kandev/kandev/internal/task/models"
)

// footprintProviderStub returns a fixed row set so the workspace filter can be
// asserted without a runtime tier behind it.
type footprintProviderStub struct {
	rows       []agentruntime.SessionRuntimeFootprint
	complete   bool
	observedAt time.Time
}

func (s *footprintProviderStub) RuntimeFootprintRows() []agentruntime.SessionRuntimeFootprint {
	return s.rows
}

func (s *footprintProviderStub) RuntimeFootprintComplete() bool { return s.complete }

func (s *footprintProviderStub) RuntimeFootprintObservedAt() time.Time { return s.observedAt }

// TestWorkspaceRuntimeFootprintScopesRowsToTheWorkspace is the security-relevant
// case: the projection is installation-wide, so a workspace-scoped read must
// drop every row whose task belongs to another workspace rather than filtering
// only the totals.
func TestWorkspaceRuntimeFootprintScopesRowsToTheWorkspace(t *testing.T) {
	svc := setupFootprintService(t, "workspace-a", "task-a1", "task-a2")
	svc.SetRuntimeFootprintProvider(&footprintProviderStub{
		complete:   true,
		observedAt: time.Now().UTC(),
		rows: []agentruntime.SessionRuntimeFootprint{
			{SessionID: "session-a1", TaskID: "task-a1", Processes: 4, CommittedBytes: 1000, ResidentBytes: 600},
			{SessionID: "session-a2", TaskID: "task-a2", Processes: 2, CommittedBytes: 300, ResidentBytes: 200},
			{SessionID: "session-b1", TaskID: "task-b1", Processes: 9, CommittedBytes: 9000, ResidentBytes: 8000},
			{SessionID: "session-b2", TaskID: "task-b2", Processes: 7, CommittedBytes: 7000, ResidentBytes: 6000},
		},
	})

	view, err := svc.WorkspaceRuntimeFootprint(context.Background(), "workspace-a")
	if err != nil {
		t.Fatalf("WorkspaceRuntimeFootprint: %v", err)
	}

	if len(view.Runtimes) != 2 {
		t.Fatalf("len(Runtimes) = %d, want only this workspace's rows", len(view.Runtimes))
	}
	for _, row := range view.Runtimes {
		if row.TaskID != "task-a1" && row.TaskID != "task-a2" {
			t.Fatalf("row for task %q leaked another workspace's runtime", row.TaskID)
		}
	}
	if view.WorkspaceRuntimes != 2 || view.Processes != 6 {
		t.Fatalf("workspace totals = %d runtimes / %d processes, want 2 / 6", view.WorkspaceRuntimes, view.Processes)
	}
	if view.CommittedBytes != 1300 || view.ResidentBytes != 800 {
		t.Fatalf("workspace bytes = %d/%d, want 1300/800", view.CommittedBytes, view.ResidentBytes)
	}
	// The installation-wide count is context for the rows, not a workspace total,
	// so it must still reflect everything the observation saw.
	if view.LiveRuntimes != 4 {
		t.Fatalf("LiveRuntimes = %d, want the installation-wide observation size 4", view.LiveRuntimes)
	}
}

// TestWorkspaceRuntimeFootprintExcludesUnattributableRow pins that a row with no
// task identity cannot be attributed to any workspace. It stays in the
// installation totals and is not shown in a workspace view, because guessing an
// owner would be wrong in both directions.
func TestWorkspaceRuntimeFootprintExcludesUnattributableRow(t *testing.T) {
	svc := setupFootprintService(t, "workspace-a", "task-a1")
	svc.SetRuntimeFootprintProvider(&footprintProviderStub{
		complete: true,
		rows: []agentruntime.SessionRuntimeFootprint{
			{SessionID: "session-a1", TaskID: "task-a1", Processes: 2, ResidentBytes: 100},
			{SessionID: "session-unknown", TaskID: "", Processes: 5, ResidentBytes: 900},
		},
	})

	view, err := svc.WorkspaceRuntimeFootprint(context.Background(), "workspace-a")
	if err != nil {
		t.Fatalf("WorkspaceRuntimeFootprint: %v", err)
	}
	if len(view.Runtimes) != 1 || view.Runtimes[0].SessionID != "session-a1" {
		t.Fatalf("Runtimes = %+v, want only the attributable row", view.Runtimes)
	}
	if view.LiveRuntimes != 2 {
		t.Fatalf("LiveRuntimes = %d, want the unattributable row still counted installation-wide", view.LiveRuntimes)
	}
}

// TestWorkspaceRuntimeFootprintCarriesUnreadableMarker pins that a partially
// measured runtime stays visibly partial in the workspace view.
func TestWorkspaceRuntimeFootprintCarriesUnreadableMarker(t *testing.T) {
	svc := setupFootprintService(t, "workspace-a", "task-a1")
	svc.SetRuntimeFootprintProvider(&footprintProviderStub{
		complete: true,
		rows: []agentruntime.SessionRuntimeFootprint{
			{SessionID: "session-a1", TaskID: "task-a1", Processes: 3, ResidentBytes: 100, UnreadableProcesses: 2},
		},
	})

	view, err := svc.WorkspaceRuntimeFootprint(context.Background(), "workspace-a")
	if err != nil {
		t.Fatalf("WorkspaceRuntimeFootprint: %v", err)
	}
	if view.UnreadableRuntimes != 1 {
		t.Fatalf("UnreadableRuntimes = %d, want 1", view.UnreadableRuntimes)
	}
	if len(view.Runtimes) != 1 || view.Runtimes[0].UnreadableProcesses != 2 {
		t.Fatalf("row lost the unreadable marker: %+v", view.Runtimes)
	}
}

// TestWorkspaceRuntimeFootprintReportsIncompleteObservation pins that a failed
// observation is not presented as a current measurement.
func TestWorkspaceRuntimeFootprintReportsIncompleteObservation(t *testing.T) {
	svc := setupFootprintService(t, "workspace-a", "task-a1")
	svc.SetRuntimeFootprintProvider(&footprintProviderStub{complete: false})

	view, err := svc.WorkspaceRuntimeFootprint(context.Background(), "workspace-a")
	if err != nil {
		t.Fatalf("WorkspaceRuntimeFootprint: %v", err)
	}
	if view.Complete {
		t.Fatal("an incomplete observation must not be reported as complete")
	}
	if len(view.Runtimes) != 0 {
		t.Fatalf("an incomplete observation published %d rows", len(view.Runtimes))
	}
}

// TestWorkspaceRuntimeFootprintWithoutProvider pins that an installation with no
// footprint source reports no measurement rather than an empty reading that looks
// like zero memory in use.
func TestWorkspaceRuntimeFootprintWithoutProvider(t *testing.T) {
	svc := setupFootprintService(t, "workspace-a", "task-a1")

	view, err := svc.WorkspaceRuntimeFootprint(context.Background(), "workspace-a")
	if err != nil {
		t.Fatalf("WorkspaceRuntimeFootprint: %v", err)
	}
	if view.Complete {
		t.Fatal("no provider must not report a complete measurement")
	}
	if len(view.Runtimes) != 0 {
		t.Fatalf("no provider published %d rows", len(view.Runtimes))
	}
}

// TestWorkspaceRuntimeFootprintOfAnotherWorkspaceYieldsNoRows pins the
// cross-workspace boundary from the caller's side. Reaching this service is not
// itself the guard when auth is disabled, because a synthetic identity is
// deliberately unscoped; the filter is. A workspace that owns none of the rows
// must come back empty rather than carrying another workspace's runtimes.
func TestWorkspaceRuntimeFootprintOfAnotherWorkspaceYieldsNoRows(t *testing.T) {
	svc := setupFootprintService(t, "workspace-a", "task-a1")
	svc.SetRuntimeFootprintProvider(&footprintProviderStub{
		complete: true,
		rows: []agentruntime.SessionRuntimeFootprint{
			{SessionID: "session-a1", TaskID: "task-a1", Processes: 4, ResidentBytes: 600},
			{SessionID: "session-b1", TaskID: "task-b1", Processes: 9, ResidentBytes: 900},
		},
	})

	view, err := svc.WorkspaceRuntimeFootprint(context.Background(), "workspace-b")
	if err != nil {
		t.Fatalf("WorkspaceRuntimeFootprint: %v", err)
	}
	if len(view.Runtimes) != 0 || view.WorkspaceRuntimes != 0 || view.ResidentBytes != 0 {
		t.Fatalf("workspace-b saw rows it does not own: %+v", view)
	}
	// The installation-wide observation is still the context it was measured in,
	// which is why the two must not be conflated.
	if view.LiveRuntimes != 2 {
		t.Fatalf("LiveRuntimes = %d, want the installation-wide observation size 2", view.LiveRuntimes)
	}
}

// setupFootprintService returns a task service over a real repository holding one
// workspace and the given tasks, so the workspace filter and the authorization
// boundary are exercised against real rows rather than a hand-written fake.
func setupFootprintService(t *testing.T, workspaceID string, taskIDs ...string) *Service {
	t.Helper()
	svc, _, repo := createTestService(t)
	ctx := context.Background()

	profileID := "footprint-test-profile"
	if err := repo.CreateWorkspace(ctx, &models.Workspace{
		ID: workspaceID, Name: workspaceID, DefaultAgentProfileID: &profileID,
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	for _, taskID := range taskIDs {
		if err := repo.CreateTask(ctx, &models.Task{
			ID: taskID, WorkspaceID: workspaceID, Title: taskID,
		}); err != nil {
			t.Fatalf("create task %s: %v", taskID, err)
		}
	}
	return svc
}
