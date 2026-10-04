package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

func newPeerReportFixture(t *testing.T) (*Service, *mockAgentManager, chan struct{}) {
	t.Helper()
	svc, agentMgr, promptDone, repo := newPeerReportFixtureWithRepo(t)
	svc.messageQueue = newAuthoritativeMemoryQueue(repo, testLogger())
	return svc, agentMgr, promptDone
}

func newPeerReportFixtureWithRepo(t *testing.T) (*Service, *mockAgentManager, chan struct{}, *sqliterepo.Repository) {
	t.Helper()
	ctx := context.Background()
	repo := setupTestRepo(t)
	seedSession(t, repo, "t-peer", "s-peer", "step1")
	seedExecutorRunning(t, repo, "s-peer", "t-peer", "exec-peer")
	session, err := repo.GetTaskSession(ctx, "s-peer")
	require.NoError(t, err)
	session.State = models.TaskSessionStateWaitingForInput
	require.NoError(t, repo.UpdateTaskSession(ctx, session))

	promptDone := make(chan struct{})
	agentMgr := &mockAgentManager{isAgentRunning: true, repoForExecutionLookup: repo, promptDone: promptDone}
	svc := createTestServiceWithAgent(repo, newMockStepGetter(), newMockTaskRepo(), agentMgr)
	svc.executor = executor.NewExecutor(agentMgr, repo, testLogger(), executor.ExecutorConfig{})
	return svc, agentMgr, promptDone, repo
}

func queuePeerReport(t *testing.T, svc *Service, sender, content string) {
	t.Helper()
	_, err := svc.messageQueue.QueueMessageWithMetadata(
		context.Background(),
		"s-peer",
		"t-peer",
		content,
		"",
		messagequeue.QueuedByAgent,
		false,
		nil,
		map[string]interface{}{messagequeue.MetadataSenderTaskID: sender},
	)
	require.NoError(t, err)
}

func waitForQueuedPrompt(t *testing.T, promptDone chan struct{}) {
	t.Helper()
	select {
	case <-promptDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the queued prompt")
	}
}

func TestDrainQueuedMessage_DeliversChildReportBurstAsOnePrompt(t *testing.T) {
	svc, agentMgr, promptDone := newPeerReportFixture(t)
	queuePeerReport(t, svc, "child-a", "report one")
	queuePeerReport(t, svc, "child-b", "report two")
	queuePeerReport(t, svc, "child-c", "report three")

	require.True(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"))
	waitForQueuedPrompt(t, promptDone)

	require.Len(t, agentMgr.capturedPrompts, 1)
	prompt := agentMgr.capturedPrompts[0]
	require.Contains(t, prompt, "from task child-a")
	require.Contains(t, prompt, "from task child-b")
	require.Contains(t, prompt, "from task child-c")
	require.Contains(t, prompt, "report one")
	require.Contains(t, prompt, "report three")
	// An ordinary reserve removes its row when the batch is taken, so an empty
	// queue here reports the reserve, not the settlement. Claim settlement for
	// the same batch is asserted against SQLite in the messagequeue package.
	require.Zero(t, svc.messageQueue.GetStatus(context.Background(), "s-peer").Count)
	require.False(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"),
		"the whole burst was consumed by one turn")
}

func TestDrainQueuedMessage_KeepsUserInstructionBehindReports(t *testing.T) {
	svc, agentMgr, promptDone := newPeerReportFixture(t)
	queuePeerReport(t, svc, "child-a", "report one")
	_, err := svc.messageQueue.QueueMessage(
		context.Background(), "s-peer", "t-peer", "operator instruction", "", "user", false, nil)
	require.NoError(t, err)
	queuePeerReport(t, svc, "child-b", "report two")

	require.True(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"))
	waitForQueuedPrompt(t, promptDone)
	require.Len(t, agentMgr.capturedPrompts, 1)
	require.Contains(t, agentMgr.capturedPrompts[0], "report one")
	require.NotContains(t, agentMgr.capturedPrompts[0], "report two")
	require.NotContains(t, agentMgr.capturedPrompts[0], "operator instruction")
	require.Equal(t, 2, svc.messageQueue.GetStatus(context.Background(), "s-peer").Count)
}

// peerReportClaimRepository is the full dispatch-claim contract a durable queue
// repository offers, which the memory repository deliberately does not.
type peerReportClaimRepository interface {
	messagequeue.Repository
	ListPendingQueueDispatches(context.Context) ([]messagequeue.PendingQueueDispatch, error)
	MarkPendingQueueDispatchAccepted(context.Context, *messagequeue.QueuedMessage) error
	RewritePendingQueueDispatchMessage(context.Context, *messagequeue.QueuedMessage) error
	DeletePendingQueueDispatch(context.Context, *messagequeue.QueuedMessage) error
}

type recordedClaimOperation struct {
	operation string
	entryID   string
	payload   string
}

type recordingPeerReportClaimRepository struct {
	peerReportClaimRepository
	mu         sync.Mutex
	operations []recordedClaimOperation
}

func (r *recordingPeerReportClaimRepository) record(operation string, msg *messagequeue.QueuedMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := recordedClaimOperation{operation: operation, entryID: msg.ID}
	if operation == "rewrite" {
		entry.payload = msg.Content
	}
	r.operations = append(r.operations, entry)
}

func (r *recordingPeerReportClaimRepository) RewritePendingQueueDispatchMessage(
	ctx context.Context,
	msg *messagequeue.QueuedMessage,
) error {
	r.record("rewrite", msg)
	return r.peerReportClaimRepository.RewritePendingQueueDispatchMessage(ctx, msg)
}

func (r *recordingPeerReportClaimRepository) MarkPendingQueueDispatchAccepted(
	ctx context.Context,
	msg *messagequeue.QueuedMessage,
) error {
	r.record("accept", msg)
	return r.peerReportClaimRepository.MarkPendingQueueDispatchAccepted(ctx, msg)
}

func (r *recordingPeerReportClaimRepository) DeletePendingQueueDispatch(
	ctx context.Context,
	msg *messagequeue.QueuedMessage,
) error {
	r.record("delete", msg)
	return r.peerReportClaimRepository.DeletePendingQueueDispatch(ctx, msg)
}

func (r *recordingPeerReportClaimRepository) recorded() []recordedClaimOperation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedClaimOperation(nil), r.operations...)
}

func newSQLitePeerReportFixture(t *testing.T) (*Service, *mockAgentManager, chan struct{}, *recordingPeerReportClaimRepository) {
	t.Helper()
	svc, agentMgr, promptDone, repo := newPeerReportFixtureWithRepo(t)
	db := sqlx.NewDb(repo.DB(), "sqlite3")
	persistentRepo, err := messagequeue.NewSQLiteRepository(db, db)
	require.NoError(t, err)
	claimRepo, ok := persistentRepo.(peerReportClaimRepository)
	require.True(t, ok, "the SQLite queue repository must persist dispatch claims")
	recorder := &recordingPeerReportClaimRepository{peerReportClaimRepository: claimRepo}
	svc.messageQueue = messagequeue.NewService(recorder, messagequeue.DefaultMaxPerSession, testLogger())
	return svc, agentMgr, promptDone, recorder
}

// peerReportSettlementOnlyRepository persists dispatch claims but cannot rewrite
// an unsettled one, so a folded batch would replay only its leading report after
// a restart.
type peerReportSettlementOnlyRepository struct {
	messagequeue.Repository
}

func (r *peerReportSettlementOnlyRepository) ListPendingQueueDispatches(context.Context) ([]messagequeue.PendingQueueDispatch, error) {
	return nil, nil
}

func (r *peerReportSettlementOnlyRepository) MarkPendingQueueDispatchAccepted(context.Context, *messagequeue.QueuedMessage) error {
	return nil
}

func (r *peerReportSettlementOnlyRepository) DeletePendingQueueDispatch(context.Context, *messagequeue.QueuedMessage) error {
	return nil
}

func TestDrainQueuedMessage_DeliversOneReportAtATimeWithoutClaimRewrite(t *testing.T) {
	svc, agentMgr, promptDone, recorder := newSQLitePeerReportFixture(t)
	svc.messageQueue = messagequeue.NewService(
		&peerReportSettlementOnlyRepository{Repository: recorder.peerReportClaimRepository},
		messagequeue.DefaultMaxPerSession,
		testLogger(),
	)
	queuePeerReport(t, svc, "child-a", "report one")
	queuePeerReport(t, svc, "child-b", "report two")

	require.True(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"))
	waitForQueuedPrompt(t, promptDone)

	require.Len(t, agentMgr.capturedPrompts, 1)
	require.Contains(t, agentMgr.capturedPrompts[0], "report one")
	require.NotContains(t, agentMgr.capturedPrompts[0], "report two",
		"folding requires a rewritable claim")
	require.Equal(t, 1, svc.messageQueue.GetStatus(context.Background(), "s-peer").Count,
		"the folded row returns to the head instead of being lost")
}

func TestDrainQueuedMessage_RecordsTheComposedBatchBeforeSettlingFoldedClaims(t *testing.T) {
	svc, _, promptDone, recorder := newSQLitePeerReportFixture(t)
	queuePeerReport(t, svc, "child-a", "report one")
	queuePeerReport(t, svc, "child-b", "report two")
	queuePeerReport(t, svc, "child-c", "report three")

	require.True(t, svc.drainQueuedMessageForPromptableSession(context.Background(), "s-peer"))
	waitForQueuedPrompt(t, promptDone)

	operations := recorder.recorded()
	require.NotEmpty(t, operations)
	require.Equal(t, "rewrite", operations[0].operation,
		"the leading claim must carry the composed prompt before any folded claim is settled")
	require.Contains(t, operations[0].payload, "report one")
	require.Contains(t, operations[0].payload, "report two")
	require.Contains(t, operations[0].payload, "report three")
	require.GreaterOrEqual(t, len(operations), 3,
		"one rewrite and one acceptance per folded row settle the whole batch before the worker runs")
	leadingID := operations[0].entryID
	for index, operation := range operations[1:] {
		if operation.entryID == leadingID {
			// The leading claim is settled later by the ordinary post-dispatch
			// path, which runs after the worker accepted the composed prompt.
			require.GreaterOrEqual(t, index, 2,
				"the leading claim is settled after every folded row")
			continue
		}
		require.Less(t, index, 2, "every folded row's claim is settled before the worker runs")
		require.Equal(t, "accept", operation.operation)
	}
}
