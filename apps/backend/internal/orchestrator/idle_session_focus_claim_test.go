package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// A task-scoped context cannot authorize a different destination or a lost lease.
func TestIdleFocusResumeRequiresExactDispatchClaim(t *testing.T) {
	for _, change := range []string{
		"valid", "missing", "foreign_task", "foreign_session", "unheld", "foreign_service",
		"expired", "successor", "foreign_owner", "wrong_kind", "route_changed",
	} {
		t.Run(change, func(t *testing.T) { testIdleFocusDispatchClaim(t, change) })
	}
}

func testIdleFocusDispatchClaim(t *testing.T, change string) {
	t.Helper()
	ctx := context.Background()
	f := newFocusCeilingFixture(t, "focus-claim-task", "focus-claim-session", models.TaskSessionStateCompleted)
	if _, _, _, err := f.svc.focusTaskSession(ctx, "focus-claim-task", "focus-claim-session"); err != nil {
		t.Fatal(err)
	}
	claim, found, err := f.svc.claimCeilingDeferredLaunch(ctx, "focus-claim-task", "focus-claim-session", ceilingClaimOwnerReplay)
	if err != nil || !found || claim == nil {
		t.Fatalf("claim: found=%v err=%v", found, err)
	}
	originalClaim := *claim
	defer originalClaim.releaseIfHeld(ctx)
	changeIdleFocusDispatchClaim(t, f.svc, claim, change)
	session, err := f.svc.repo.GetTaskSession(ctx, "focus-claim-session")
	if err != nil {
		t.Fatal(err)
	}
	running, err := f.svc.repo.GetExecutorRunningBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if change == "missing" {
		claim = nil
	}
	_, err = f.svc.idleSuspensionResumeShortcut(withCeilingDispatchClaim(ctx, claim), session.TaskID, session.ID, session, running)
	if change == "valid" {
		if err != nil {
			t.Fatalf("owned queued focus resume: %v", err)
		}
	} else if !errors.Is(err, ErrIdleSuspensionProvenanceRequired) {
		t.Fatalf("%s claim authorized focus resume: %v", change, err)
	}
	if f.launchCalls.Load() != 0 {
		t.Fatal("provenance inspection launched an agent")
	}
}

func changeIdleFocusDispatchClaim(t *testing.T, svc *Service, claim *ceilingDeferredLaunchClaim, change string) {
	t.Helper()
	ctx := context.Background()
	switch change {
	case "foreign_task":
		claim.taskID = "another-task"
	case "foreign_session":
		claim.deferral.Payload[metaKeySessionID] = "another-session"
	case "unheld":
		claim.held = false
	case "foreign_service":
		claim.svc = &Service{}
	case "wrong_kind":
		claim.deferral.Kind = models.CeilingLaunchPromptEnsure
	case "expired", "successor", "foreign_owner":
		record, _, err := svc.repo.GetTaskDeferredLaunch(ctx, "focus-claim-task")
		if err != nil {
			t.Fatal(err)
		}
		marker := record[models.CeilingLaunchClaimKey].(map[string]interface{})
		if change == "expired" {
			marker[models.CeilingLaunchClaimExpiresAtKey] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
		}
		if change == "successor" {
			marker["id"] = "successor-claim"
		}
		if change == "foreign_owner" {
			marker["owner"] = ceilingClaimOwnerSendNow
		}
		if err := svc.repo.SetTaskMetadataKey(ctx, "focus-claim-task", models.MetaKeyDeferredLaunch, record); err != nil {
			t.Fatal(err)
		}
	case "route_changed":
		session, err := svc.repo.GetTaskSession(ctx, "focus-claim-session")
		if err != nil {
			t.Fatal(err)
		}
		session.RouteState = "retiring"
		if err := svc.repo.UpdateTaskSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeferredIdleFocusResumeDropsLostProvenance(t *testing.T) {
	ctx := context.Background()
	f := newFocusCeilingFixture(t, "focus-unparked-task", "focus-unparked-session", models.TaskSessionStateCompleted)
	if _, _, _, err := f.svc.focusTaskSession(ctx, "focus-unparked-task", "focus-unparked-session"); err != nil {
		t.Fatal(err)
	}
	running, err := f.svc.repo.GetExecutorRunningBySessionID(ctx, "focus-unparked-session")
	if err != nil {
		t.Fatal(err)
	}
	running.IdleSuspensionState = models.ExecutorIdleSuspensionNone
	if err := f.svc.repo.UpsertExecutorRunning(ctx, running); err != nil {
		t.Fatal(err)
	}
	f.svc.sessionCeiling.release(f.occupierKey)
	f.svc.retryOneDeferredCeilingLaunch(ctx, &models.Task{ID: "focus-unparked-task"})
	f.svc.retryOneDeferredCeilingLaunch(ctx, &models.Task{ID: "focus-unparked-task"})
	if f.launchCalls.Load() != 0 {
		t.Fatal("unparked destination launched an agent")
	}
	record, _, err := f.svc.repo.GetTaskDeferredLaunch(ctx, "focus-unparked-task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := models.ReadCeilingDeferral(record); err == nil {
		t.Fatal("lost-provenance resume remained queued")
	}
}
