package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// TestDeferCeilingRefusalCreatesRecordFromAbsent covers the first refusal for a
// task with no deferred_launch value at all: AC-11's create case.
func TestDeferCeilingRefusalCreatesRecordFromAbsent(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{ID: "defer-absent", Title: "T"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	payload := map[string]interface{}{"prompt": "hello"}
	if err := svc.deferCeilingRefusal(ctx, "defer-absent", "", models.CeilingLaunchStart, payload, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal: %v", err)
	}

	record := deferredLaunchOf(t, svc, "defer-absent")
	if record == nil {
		t.Fatal("no deferred_launch record was written")
	}
	if record[models.CeilingDeferredKey] != true {
		t.Fatalf("ceiling_deferred not set: %+v", record)
	}
	if record[models.CeilingLaunchKindKey] != string(models.CeilingLaunchStart) {
		t.Fatalf("ceiling_launch_kind = %v, want %q", record[models.CeilingLaunchKindKey], models.CeilingLaunchStart)
	}
	nested, _ := record[models.CeilingLaunchPayloadKey].(map[string]interface{})
	if nested["prompt"] != "hello" {
		t.Fatalf("ceiling_launch_payload did not carry the payload: %+v", record)
	}
	if record[models.CeilingQueuedAtKey] == nil || record[models.CeilingQueuedAtKey] == "" {
		t.Fatalf("ceiling_queued_at was not stamped: %+v", record)
	}
}

func TestReconcileQueuedTaskStateRepairsLegacyInProgressRow(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, "defer-state-repair", v1.TaskStateInProgress)
	svc.taskRepo = taskRepo
	ctx := context.Background()
	queuedAt := time.Now().UTC()
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "defer-state-repair", Title: "T", State: v1.TaskStateInProgress,
		CreatedAt: queuedAt, UpdatedAt: queuedAt,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := repo.CreateTaskSession(ctx, &models.TaskSession{
		ID: "defer-state-repair-session", TaskID: "defer-state-repair",
		State: models.TaskSessionStateCreated, StartedAt: queuedAt, UpdatedAt: queuedAt,
	}); err != nil {
		t.Fatalf("CreateTaskSession: %v", err)
	}
	if err := repo.SetTaskMetadataKey(ctx, "defer-state-repair", models.MetaKeyDeferredLaunch,
		models.CeilingRecordKeys(models.CeilingDeferral{
			Kind:    models.CeilingLaunchStartCreated,
			Payload: map[string]interface{}{metaKeySessionID: "defer-state-repair-session"},
			Origin:  string(launchOriginAutomatic), ReasonCode: ceilingReasonRefused,
			QueuedAt: queuedAt, Ceiling: 5, Population: 6, PopulationKnown: true,
		})); err != nil {
		t.Fatalf("SetTaskMetadataKey: %v", err)
	}

	svc.reconcileQueuedTaskState(ctx, "defer-state-repair")
	if taskRepo.updatedStates["defer-state-repair"] != v1.TaskStateScheduling {
		t.Fatalf("state repair write = %q, want %q", taskRepo.updatedStates["defer-state-repair"], v1.TaskStateScheduling)
	}
}

// TestDeferCeilingRefusalMergesOntoExistingWIPIntent pins AC-46a/AC-12b: a
// ceiling refusal must not clobber a pre-existing start_when_unblocked intent.
func TestDeferCeilingRefusalMergesOntoExistingWIPIntent(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "defer-merge", Title: "T",
		Metadata: map[string]interface{}{models.MetaKeyDeferredLaunch: map[string]interface{}{
			models.DeferredLaunchStartWhenUnblockedKey: true,
			"user_id": "u1",
		}},
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := svc.deferCeilingRefusal(ctx, "defer-merge", "", models.CeilingLaunchStart, map[string]interface{}{"prompt": "p"}, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal: %v", err)
	}

	record := deferredLaunchOf(t, svc, "defer-merge")
	if record[models.DeferredLaunchStartWhenUnblockedKey] != true {
		t.Fatalf("start_when_unblocked was clobbered: %+v", record)
	}
	if record["user_id"] != "u1" {
		t.Fatalf("user_id was clobbered: %+v", record)
	}
	if record[models.CeilingDeferredKey] != true {
		t.Fatalf("ceiling_deferred was not added: %+v", record)
	}
}

// TestDeferCeilingRefusalIsANoOpOnByteIdenticalDuplicate pins AC-12a/AC-32: a
// second refusal that is the same launch must not disturb the stored record's
// queued_at.
func TestDeferCeilingRefusalIsANoOpOnByteIdenticalDuplicate(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{ID: "defer-dup", Title: "T"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	payload := map[string]interface{}{"prompt": "same"}
	if err := svc.deferCeilingRefusal(ctx, "defer-dup", "", models.CeilingLaunchStart, payload, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal (1): %v", err)
	}
	first := deferredLaunchOf(t, svc, "defer-dup")
	firstQueuedAt := first[models.CeilingQueuedAtKey]

	if err := svc.deferCeilingRefusal(ctx, "defer-dup", "", models.CeilingLaunchStart, map[string]interface{}{"prompt": "same"}, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal (2): %v", err)
	}
	second := deferredLaunchOf(t, svc, "defer-dup")
	if second[models.CeilingQueuedAtKey] != firstQueuedAt {
		t.Fatalf("queued_at changed on a byte-identical duplicate: %v -> %v", firstQueuedAt, second[models.CeilingQueuedAtKey])
	}
}

// TestDeferCeilingRefusalRetainsEarlierRecordOnDifferingDuplicate pins AC-12d:
// a second, differing automatic refusal for the same task must not overwrite the
// first launch's payload.
func TestDeferCeilingRefusalRetainsEarlierRecordOnDifferingDuplicate(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{ID: "defer-collide", Title: "T"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := svc.deferCeilingRefusal(ctx, "defer-collide", "", models.CeilingLaunchStart, map[string]interface{}{"prompt": "first"}, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal (1): %v", err)
	}
	if err := svc.deferCeilingRefusal(ctx, "defer-collide", "", models.CeilingLaunchStart, map[string]interface{}{"prompt": "second"}, ceilingReasonRefused, 0, false, 0); !errors.Is(err, ErrCeilingLaunchConflict) {
		t.Fatalf("deferCeilingRefusal (2) error = %v, want ErrCeilingLaunchConflict", err)
	}

	record := deferredLaunchOf(t, svc, "defer-collide")
	nested, _ := record[models.CeilingLaunchPayloadKey].(map[string]interface{})
	if nested["prompt"] != "first" {
		t.Fatalf("the earlier record was not retained: %+v", record)
	}
}

// TestDeferCeilingRefusalReusesRecordForSameAutomationRun pins the double-start
// fix: two automatic refusals for one task that serve the same automation run
// are one queued launch even when their prompts differ (a scheduled trigger
// and a manual trigger recompose different prompts around one run). The second
// refusal must reuse the stored record instead of reporting a conflict.
func TestDeferCeilingRefusalReusesRecordForSameAutomationRun(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{ID: "defer-same-run", Title: "T"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	first := map[string]interface{}{
		"prompt":                       "scheduled",
		ceilingPayloadAutomationRunKey: map[string]interface{}{"run_id": "run-1"},
	}
	if err := svc.deferCeilingRefusal(ctx, "defer-same-run", "", models.CeilingLaunchStart, first, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal (1): %v", err)
	}
	second := map[string]interface{}{
		"prompt":                       "manual",
		ceilingPayloadAutomationRunKey: map[string]interface{}{"run_id": "run-1"},
	}
	if err := svc.deferCeilingRefusal(ctx, "defer-same-run", "", models.CeilingLaunchStart, second, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal (2) error = %v, want nil (same automation run reuses the record)", err)
	}

	record := deferredLaunchOf(t, svc, "defer-same-run")
	nested, _ := record[models.CeilingLaunchPayloadKey].(map[string]interface{})
	if nested["prompt"] != "scheduled" {
		t.Fatalf("the earlier record was not retained: %+v", record)
	}
}

// TestDeferCeilingRefusalConflictsAcrossAutomationRuns pins the other side: two
// refusals that serve different automation runs stay different launches and
// keep the conflict disposition, so one run's queued start never silently
// absorbs another run's.
func TestDeferCeilingRefusalConflictsAcrossAutomationRuns(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{ID: "defer-other-run", Title: "T"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	first := map[string]interface{}{
		"prompt":                       "first",
		ceilingPayloadAutomationRunKey: map[string]interface{}{"run_id": "run-1"},
	}
	if err := svc.deferCeilingRefusal(ctx, "defer-other-run", "", models.CeilingLaunchStart, first, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal (1): %v", err)
	}
	second := map[string]interface{}{
		"prompt":                       "second",
		ceilingPayloadAutomationRunKey: map[string]interface{}{"run_id": "run-2"},
	}
	if err := svc.deferCeilingRefusal(ctx, "defer-other-run", "", models.CeilingLaunchStart, second, ceilingReasonRefused, 0, false, 0); !errors.Is(err, ErrCeilingLaunchConflict) {
		t.Fatalf("deferCeilingRefusal (2) error = %v, want ErrCeilingLaunchConflict", err)
	}
}

// TestDeferCeilingRefusalReplacesNonObjectValue pins AC-46: a non-object
// deferred_launch value is replaced wholesale by the ceiling record.
func TestDeferCeilingRefusalReplacesNonObjectValue(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "defer-nonobject", Title: "T",
		Metadata: map[string]interface{}{models.MetaKeyDeferredLaunch: "garbage"},
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := svc.deferCeilingRefusal(ctx, "defer-nonobject", "", models.CeilingLaunchStart, map[string]interface{}{"prompt": "p"}, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal: %v", err)
	}

	record := deferredLaunchOf(t, svc, "defer-nonobject")
	if record[models.CeilingDeferredKey] != true {
		t.Fatalf("non-object value was not replaced with a ceiling record: %+v", record)
	}
}

// TestDeferCeilingRefusalPublishesTaskUpdated pins the fix for a ceiling
// deferral write not publishing task.updated: without it, the WS-driven UI
// never learns a launch was queued behind the session ceiling.
func TestDeferCeilingRefusalPublishesTaskUpdated(t *testing.T) {
	svc, repo := newServiceWithRealRepo(t)
	ctx := context.Background()
	if err := repo.CreateTask(ctx, &models.Task{ID: "defer-publish", Title: "T"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	events := &capturingTaskEvents{}
	svc.SetTaskEventPublisher(events)

	if err := svc.deferCeilingRefusal(ctx, "defer-publish", "s1", models.CeilingLaunchStart,
		map[string]interface{}{"prompt": "hello"}, ceilingReasonRefused, 0, false, 0); err != nil {
		t.Fatalf("deferCeilingRefusal: %v", err)
	}

	published := events.last()
	if published == nil {
		t.Fatal("deferCeilingRefusal did not publish a task.updated event")
	}
	if published.ID != "defer-publish" {
		t.Fatalf("published task id = %q, want %q", published.ID, "defer-publish")
	}
	deferred, ok := published.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	if !ok || deferred[models.CeilingDeferredKey] != true {
		t.Fatalf("published task's deferred_launch is missing the new ceiling record: %#v",
			published.Metadata[models.MetaKeyDeferredLaunch])
	}
}
