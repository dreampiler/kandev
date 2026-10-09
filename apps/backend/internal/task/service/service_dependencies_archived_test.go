package service

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// A predecessor's verdict follows its persisted state, not its archive marker:
// archiving cannot revoke a recorded COMPLETED, cannot promote an unfinished
// task to success, and cannot soften a FAILED/CANCELLED terminal state.
func TestDependencyStatusForTask_ClassifiesByStateNotArchival(t *testing.T) {
	archivedAt := time.Now().UTC()
	cases := []struct {
		name string
		task *models.Task
		want string
	}{
		{"nil task is pending", nil, DependencyPending},
		{"completed is resolved", &models.Task{State: v1.TaskStateCompleted}, DependencyResolved},
		{"archived completed stays resolved", &models.Task{State: v1.TaskStateCompleted, ArchivedAt: &archivedAt}, DependencyResolved},
		{"archived todo stays pending", &models.Task{State: v1.TaskStateTODO, ArchivedAt: &archivedAt}, DependencyPending},
		{"archived in-progress stays pending", &models.Task{State: v1.TaskStateInProgress, ArchivedAt: &archivedAt}, DependencyPending},
		{"failed stays failed", &models.Task{State: v1.TaskStateFailed}, DependencyFailed},
		{"archived failed stays failed", &models.Task{State: v1.TaskStateFailed, ArchivedAt: &archivedAt}, DependencyFailed},
		{"archived cancelled stays failed", &models.Task{State: v1.TaskStateCancelled, ArchivedAt: &archivedAt}, DependencyFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DependencyStatusForTask(tc.task); got != tc.want {
				t.Errorf("DependencyStatusForTask() = %q; want %q", got, tc.want)
			}
		})
	}
}

// The gate propagates the state-only verdict: an archived COMPLETED predecessor
// unblocks its dependent, while an archived unfinished or failed predecessor
// keeps it blocked.
func TestDependencyGate_ClassifiesArchivedPredecessorsByState(t *testing.T) {
	cases := []struct {
		name        string
		state       v1.TaskState
		wantBlocked bool
		wantReason  string
	}{
		{"archived completed unblocks", v1.TaskStateCompleted, false, ""},
		{"archived todo stays pending", v1.TaskStateTODO, true, BlockedReasonPending},
		{"archived failed stays failed", v1.TaskStateFailed, true, BlockedReasonFailed},
		{"archived cancelled stays failed", v1.TaskStateCancelled, true, BlockedReasonFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := setupOfficeTest(t)
			svc.SetBlockerRepository(&mockBlockerRepo{})
			ctx := context.Background()
			predecessor := mustSeedTask(t, svc, "Predecessor").ID
			dependent := mustSeedTask(t, svc, "Dependent").ID
			if err := svc.AddDependency(ctx, dependent, predecessor); err != nil {
				t.Fatalf("AddDependency: %v", err)
			}
			setDependencyState(t, svc, predecessor, tc.state)
			if err := svc.tasks.ArchiveTask(ctx, predecessor); err != nil {
				t.Fatalf("ArchiveTask: %v", err)
			}

			blocked, reason, err := svc.DependencyGate(ctx, dependent)
			if err != nil {
				t.Fatalf("DependencyGate: %v", err)
			}
			if blocked != tc.wantBlocked || reason != tc.wantReason {
				t.Errorf("blocked=%v reason=%q; want blocked=%v reason=%q",
					blocked, reason, tc.wantBlocked, tc.wantReason)
			}
		})
	}
}
