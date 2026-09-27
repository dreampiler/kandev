package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
)

func seedWaitingSession(t *testing.T, repo interface {
	CreateTaskSession(ctx context.Context, session *models.TaskSession) error
}, taskID, sessionID string) {
	t.Helper()
	now := time.Now().UTC().Add(time.Second)
	if err := repo.CreateTaskSession(context.Background(), &models.TaskSession{
		ID:        sessionID,
		TaskID:    taskID,
		State:     models.TaskSessionStateWaitingForInput,
		StartedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create session %s: %v", sessionID, err)
	}
}

func TestSetPrimarySessionAloneLeavesQueueOnDemotedPrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	seedWaitingSession(t, repo, "t1", "s2")
	seedExecutorRunning(t, repo, "s2", "t1", "exec-2")
	if err := repo.SetSessionPrimary(ctx, "s1"); err != nil {
		t.Fatalf("make s1 primary: %v", err)
	}
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "queued prompt", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue prompt: %v", err)
	}

	if err := svc.SetPrimarySession(ctx, "s2"); err != nil {
		t.Fatalf("SetPrimarySession: %v", err)
	}

	if _, ok := svc.messageQueue.TakeQueued(ctx, "s2"); ok {
		t.Fatal("SetPrimarySession moved the queue; the transferring variant is required for that")
	}
	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); !ok {
		t.Fatal("expected the queue to remain stranded on the demoted primary")
	}
}

func TestSetPrimarySessionTransferringQueueMovesQueueToNewPrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	seedWaitingSession(t, repo, "t1", "s2")
	seedExecutorRunning(t, repo, "s2", "t1", "exec-2")
	if err := repo.SetSessionPrimary(ctx, "s1"); err != nil {
		t.Fatalf("make s1 primary: %v", err)
	}
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "queued prompt", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue prompt: %v", err)
	}

	if err := svc.SetPrimarySessionTransferringQueue(ctx, "s2"); err != nil {
		t.Fatalf("SetPrimarySessionTransferringQueue: %v", err)
	}

	moved, ok := svc.messageQueue.TakeQueued(ctx, "s2")
	if !ok || moved.Content != "queued prompt" {
		t.Fatalf("queue on new primary = %#v, ok=%t; want the queued prompt", moved, ok)
	}
	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); ok {
		t.Fatal("queue remained on the demoted primary")
	}
}

func TestSetPrimarySessionTransferringQueueWithoutPreviousPrimaryKeepsQueue(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "queued prompt", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue prompt: %v", err)
	}

	if err := svc.SetPrimarySessionTransferringQueue(ctx, "s1"); err != nil {
		t.Fatalf("SetPrimarySessionTransferringQueue: %v", err)
	}

	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); !ok {
		t.Fatal("queue moved even though the promoted session was already primary")
	}
}

func TestRecoverStrandedQueueToPrimaryMovesTakenMessageAndLeftoverQueue(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateFailed)
	seedWaitingSession(t, repo, "t1", "s2")
	seedExecutorRunning(t, repo, "s2", "t1", "exec-2")
	if err := repo.SetSessionPrimary(ctx, "s2"); err != nil {
		t.Fatalf("make s2 primary: %v", err)
	}
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	svc.messageQueue.SetMergeEnabled(false)
	svc.messageQueue.SetAutoMergeEnabled(false)
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "leftover", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue leftover prompt: %v", err)
	}
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "in-flight", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue in-flight prompt: %v", err)
	}
	taken, ok := svc.messageQueue.TakeQueued(ctx, "s1")
	if !ok || taken.Content != "leftover" {
		t.Fatalf("take queued prompt = %#v, ok=%t", taken, ok)
	}

	if !svc.recoverStrandedQueueToPrimary(ctx, taken) {
		t.Fatal("recoverStrandedQueueToPrimary = false, want true")
	}

	recovered := map[string]bool{}
	for {
		msg, ok := svc.messageQueue.TakeQueued(ctx, "s2")
		if !ok {
			break
		}
		recovered[msg.Content] = true
	}
	if !recovered["in-flight"] || !recovered["leftover"] {
		t.Fatalf("recovered messages = %#v, want both in-flight and leftover", recovered)
	}
	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); ok {
		t.Fatal("queue remained on the stranded session")
	}
}

func TestRecoverStrandedQueueToPrimaryWithoutLivePrimaryDeclines(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateFailed)
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "in-flight", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue prompt: %v", err)
	}
	taken, ok := svc.messageQueue.TakeQueued(ctx, "s1")
	if !ok {
		t.Fatal("take queued prompt")
	}

	if svc.recoverStrandedQueueToPrimary(ctx, taken) {
		t.Fatal("recoverStrandedQueueToPrimary = true without a live primary, want false")
	}
	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); ok {
		t.Fatal("stranded helper moved a message despite no live primary")
	}
}

func TestHandleQueuedMessageExecutionErrorRecoversStrandedQueueToLivePrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateFailed)
	seedWaitingSession(t, repo, "t1", "s2")
	seedExecutorRunning(t, repo, "s2", "t1", "exec-2")
	if err := repo.SetSessionPrimary(ctx, "s2"); err != nil {
		t.Fatalf("make s2 primary: %v", err)
	}
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "in-flight", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue prompt: %v", err)
	}
	taken, ok := svc.messageQueue.TakeQueued(ctx, "s1")
	if !ok {
		t.Fatal("take queued prompt")
	}

	svc.handleQueuedMessageExecutionError(ctx, "s1", taken, nil, false, false, ErrSessionRuntimeUnavailable)

	if moved, ok := svc.messageQueue.TakeQueued(ctx, "s2"); !ok || moved.Content != "in-flight" {
		t.Fatalf("recovered message on live primary = %#v, ok=%t", moved, ok)
	}
	if _, ok := svc.messageQueue.TakeQueued(ctx, "s1"); ok {
		t.Fatal("stranded queue was not recovered off the dead session")
	}
}

func TestHandleQueuedMessageExecutionErrorWithoutLivePrimaryRequeuesInPlace(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedTaskAndSession(t, repo, "t1", "s1", models.TaskSessionStateFailed)
	svc := createTestService(repo, newMockStepGetter(), newMockTaskRepo())
	if _, err := svc.messageQueue.QueueMessage(
		ctx, "s1", "t1", "in-flight", "", messagequeue.QueuedByUser, false, nil,
	); err != nil {
		t.Fatalf("queue prompt: %v", err)
	}
	taken, ok := svc.messageQueue.TakeQueued(ctx, "s1")
	if !ok {
		t.Fatal("take queued prompt")
	}

	svc.handleQueuedMessageExecutionError(ctx, "s1", taken, nil, false, false, ErrSessionRuntimeUnavailable)

	if kept, ok := svc.messageQueue.TakeQueued(ctx, "s1"); !ok || kept.Content != "in-flight" {
		t.Fatalf("fallback requeue on stranded session = %#v, ok=%t", kept, ok)
	}
}
