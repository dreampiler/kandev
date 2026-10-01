package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

func TestAcceptedQueuedPromptRecoveryEvaluatesSuccessor(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	seedExecutorRunning(t, repo, "s1", "t1", "exec-1")
	session, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	agentMgr := &mockAgentManager{isAgentRunning: true, repoForExecutionLookup: repo}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.messageQueue.SetAutoMergeEnabled(false)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	done := make(chan struct{}, 1)
	svc.onQueuedMessageExecutionComplete = func() { done <- struct{}{} }
	first, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "accepted", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "successor", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	session.State = models.TaskSessionStateWaitingForInput
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	reserved, ok := svc.messageQueue.ReserveQueued(ctx, "s1")
	if !ok || reserved.ID != first.ID {
		t.Fatalf("first reservation = %#v", reserved)
	}
	identity, err := svc.messageQueue.ResolveSessionIdentity(ctx, "t1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	lock, release := svc.acquireCancelInFlightGuard("s1")
	lock.Lock()
	reservation := svc.markQueuedDispatchInFlightWithIdentityLocked(identity, reserved.ID, reserved)
	lock.Unlock()
	release()
	svc.finishQueuedMessageExecution(ctx, "s1", "s1", reserved, reservation, false, false, false,
		&acceptedPromptDispatchError{err: errors.New("post-dispatch publication failed")})
	svc.clearQueuedDispatchInFlightIfCurrent("s1", reservation)
	if outcome := svc.evaluateAcceptedQueuedSuccessor(ctx, second.TaskID, second.SessionID, second.ID, reservation); outcome != queueDrainDispatched {
		t.Fatalf("successor evaluation = %v, want dispatched", outcome)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("successor dispatch did not finish")
	}
	if len(agentMgr.capturedPrompts) != 1 {
		t.Fatalf("provider prompt count = %d, want only successor", len(agentMgr.capturedPrompts))
	}
	if status := svc.messageQueue.GetStatus(ctx, "s1"); status.Count != 0 {
		t.Fatalf("remaining queue count = %d, want 0", status.Count)
	}
}

func TestAcceptedQueuedPromptRecoveryTransfersToEligiblePrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	seedExecutorRunning(t, repo, "s1", "t1", "exec-1")
	now := time.Now().UTC()
	primary := &models.TaskSession{
		ID: "s2", TaskID: "t1", State: models.TaskSessionStateWaitingForInput,
		IsPrimary: true, StartedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateTaskSession(ctx, primary); err != nil {
		t.Fatal(err)
	}
	seedExecutorRunning(t, repo, "s2", "t1", "exec-2")
	agentMgr := &mockAgentManager{isAgentRunning: true, repoForExecutionLookup: repo}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	done := make(chan struct{}, 1)
	svc.onQueuedMessageExecutionComplete = func() { done <- struct{}{} }
	queued, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "successor", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	source, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	source.State = models.TaskSessionStateFailed
	if err := repo.UpdateTaskSession(ctx, source); err != nil {
		t.Fatal(err)
	}
	identity, err := svc.messageQueue.ResolveSessionIdentity(ctx, "t1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	reservation := &queuedDispatchReservation{identity: identity}
	if outcome := svc.evaluateAcceptedQueuedSuccessor(ctx, "t1", "s1", "accepted-id", reservation); outcome != queueDrainDispatched {
		t.Fatalf("primary recovery outcome = %v, want dispatched", outcome)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("primary successor did not finish")
	}
	if len(agentMgr.capturedPrompts) != 1 || svc.messageQueue.GetStatus(ctx, "s1").Count != 0 || svc.messageQueue.GetStatus(ctx, "s2").Count != 0 {
		t.Fatalf("primary transfer failed: prompts=%d source=%d primary=%d (queue %s)",
			len(agentMgr.capturedPrompts), svc.messageQueue.GetStatus(ctx, "s1").Count,
			svc.messageQueue.GetStatus(ctx, "s2").Count, queued.ID)
	}
}

func TestAcceptedQueuedPromptRecoveryRetainsQueueWithoutPrimary(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{isAgentRunning: true})
	queued, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "later work", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	session.State = models.TaskSessionStateFailed
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	identity, err := svc.messageQueue.ResolveSessionIdentity(ctx, "t1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if outcome := svc.evaluateAcceptedQueuedSuccessor(ctx, "t1", "s1", "accepted-id", &queuedDispatchReservation{identity: identity}); outcome != queueDrainSkipped {
		t.Fatalf("terminal recovery outcome = %v, want skipped", outcome)
	}
	status := svc.messageQueue.GetStatus(ctx, "s1")
	if status.Count != 1 || status.Entries[0].ID != queued.ID {
		t.Fatalf("terminal successor was lost or replayed: %#v", status.Entries)
	}
}

func TestAcceptedQueuedPromptRecoveryHonorsAutoRunOff(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), &mockAgentManager{isAgentRunning: true})
	queued, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "later work", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	session.State = models.TaskSessionStateWaitingForInput
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := svc.messageQueue.SetAutoRun(ctx, "s1", false); err != nil {
		t.Fatal(err)
	}
	if outcome := svc.evaluateAcceptedQueuedSuccessor(ctx, "t1", "s1", "accepted-id", nil); outcome != queueDrainPaused {
		t.Fatalf("Auto-run OFF outcome = %v, want paused", outcome)
	}
	status := svc.messageQueue.GetStatus(ctx, "s1")
	if status.Count != 1 || status.Entries[0].ID != queued.ID {
		t.Fatalf("Auto-run OFF successor was not retained: %#v", status.Entries)
	}
}

func TestAcceptedQueuedPromptRecoveryRunsAfterDispatchFailure(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	seedExecutorRunning(t, repo, "s1", "t1", "exec-1")
	agentMgr := &mockAgentManager{isAgentRunning: true, repoForExecutionLookup: repo, promptAcceptedOnError: true}
	called := 0
	agentMgr.promptAgentFunc = func(context.Context, string, string, []v1.MessageAttachment, bool) (*executor.PromptResult, error) {
		called++
		if called == 1 {
			return nil, errors.New("provider accepted; local publication failed")
		}
		return &executor.PromptResult{}, nil
	}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.messageQueue.SetAutoMergeEnabled(false)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	done := make(chan struct{}, 2)
	svc.onQueuedMessageExecutionComplete = func() { done <- struct{}{} }
	first, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "accepted", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.messageQueue.QueueMessage(ctx, "s1", "t1", "successor", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	session.State = models.TaskSessionStateWaitingForInput
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	reserved, ok := svc.messageQueue.ReserveQueued(ctx, "s1")
	if !ok || reserved.ID != first.ID {
		t.Fatalf("reserved = %#v", reserved)
	}
	identity, err := svc.messageQueue.ResolveSessionIdentity(ctx, "t1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	lock, release := svc.acquireCancelInFlightGuard("s1")
	lock.Lock()
	reservation := svc.markQueuedDispatchInFlightWithIdentityLocked(identity, reserved.ID, reserved)
	lock.Unlock()
	release()
	svc.executeQueuedMessageWithReservation("s1", reserved, reservation)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("accepted dispatch did not finish")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("successor was not evaluated after accepted error")
	}
	if len(agentMgr.capturedPrompts) != 2 || svc.messageQueue.GetStatus(ctx, "s1").Count != 0 {
		t.Fatalf("post-error prompts=%d queue=%d, want accepted and successor once", len(agentMgr.capturedPrompts), svc.messageQueue.GetStatus(ctx, "s1").Count)
	}
}

func TestAcceptedQueuedPromptRecoveryConcurrentReadyDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", "step1")
	seedExecutorRunning(t, repo, "s1", "t1", "exec-1")
	started := make(chan struct{})
	releasePrompt := make(chan struct{})
	agentMgr := &mockAgentManager{isAgentRunning: true, repoForExecutionLookup: repo}
	agentMgr.promptAgentFunc = func(context.Context, string, string, []v1.MessageAttachment, bool) (*executor.PromptResult, error) {
		close(started)
		<-releasePrompt
		return &executor.PromptResult{}, nil
	}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	done := make(chan struct{}, 1)
	svc.onQueuedMessageExecutionComplete = func() { done <- struct{}{} }
	_, err := svc.messageQueue.QueueMessage(ctx, "s1", "t1", "successor", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	session.State = models.TaskSessionStateWaitingForInput
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if outcome := svc.evaluateAcceptedQueuedSuccessor(ctx, "t1", "s1", "accepted-id", nil); outcome != queueDrainDispatched {
		t.Fatalf("successor outcome = %v, want dispatched", outcome)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("successor prompt did not start")
	}
	readyDrain := make(chan bool, 1)
	go func() { readyDrain <- svc.drainQueuedMessageForPromptableSession(ctx, "s1") }()
	close(releasePrompt)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("successor prompt did not finish")
	}
	select {
	case dispatched := <-readyDrain:
		if dispatched {
			t.Fatal("concurrent ready signal claimed a second queue entry")
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent ready signal did not settle")
	}
	if len(agentMgr.capturedPrompts) != 1 || svc.messageQueue.GetStatus(ctx, "s1").Count != 0 {
		t.Fatalf("concurrent ready produced prompts=%d queue=%d", len(agentMgr.capturedPrompts), svc.messageQueue.GetStatus(ctx, "s1").Count)
	}
}

func TestAcceptedQueuedPromptRecoveryRestartKeepsSuccessor(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/queue.db"
	queue, db := newWorkflowTransferQueue(t, dbPath)
	queue.SetAutoMergeEnabled(false)
	first, err := queue.QueueMessage(ctx, "session-1", "task-1", "accepted", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := queue.QueueMessage(ctx, "session-1", "task-1", "successor", "", messagequeue.QueuedByUser, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	reserved, ok := queue.ReserveQueued(ctx, "session-1")
	if !ok || reserved.ID != first.ID {
		t.Fatalf("reserved = %#v", reserved)
	}
	if err := queue.MarkPendingQueueDispatchAccepted(ctx, reserved); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	restartedQueue, restartedDB := newWorkflowTransferQueue(t, dbPath)
	t.Cleanup(func() { _ = restartedDB.Close() })
	restarted := &Service{logger: testLogger(), messageQueue: restartedQueue}
	if err := restarted.reconcilePendingQueueDispatchesOnStartup(ctx); err != nil {
		t.Fatal(err)
	}
	status := restartedQueue.GetStatus(ctx, "session-1")
	if status.Count != 1 || status.Entries[0].ID != second.ID {
		t.Fatalf("restarted queue = %#v, want only successor %s", status.Entries, second.ID)
	}
}
