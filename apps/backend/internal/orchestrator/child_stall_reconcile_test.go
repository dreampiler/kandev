package orchestrator

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

var childStallReconcileNow = time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC)

// childStallStoreFake implements ChildStallStore for the reconcile tests. It
// answers only the reads the reconcile path makes, from the maps it is given.
type childStallStoreFake struct {
	tasks    map[string]*models.Task
	sessions map[string]*models.TaskSession
	turns    map[string]*models.Turn
	listed   []*models.Turn
}

func newChildStallStoreFake() *childStallStoreFake {
	return &childStallStoreFake{
		tasks:    map[string]*models.Task{},
		sessions: map[string]*models.TaskSession{},
		turns:    map[string]*models.Turn{},
	}
}

func (f *childStallStoreFake) ListUnresolvedChildStallTurns(context.Context, time.Time, int) ([]*models.Turn, error) {
	return f.listed, nil
}

func (f *childStallStoreFake) GetLatestCompletedTurnBySessionID(context.Context, string) (*models.Turn, error) {
	return nil, nil
}

func (f *childStallStoreFake) GetActiveTurnBySessionID(context.Context, string) (*models.Turn, error) {
	return nil, sql.ErrNoRows
}

func (f *childStallStoreFake) GetTurn(_ context.Context, id string) (*models.Turn, error) {
	return f.turns[id], nil
}

func (f *childStallStoreFake) PatchTurnMetadata(
	_ context.Context, _, turnID string, updates map[string]interface{},
) (bool, time.Time, error) {
	turn := f.turns[turnID]
	if turn == nil {
		return false, time.Time{}, nil
	}
	if turn.Metadata == nil {
		turn.Metadata = map[string]interface{}{}
	}
	for key, value := range updates {
		turn.Metadata[key] = value
	}
	return true, childStallReconcileNow, nil
}

func (f *childStallStoreFake) SetSessionMetadataKey(context.Context, string, string, interface{}) error {
	return nil
}

func (f *childStallStoreFake) GetTask(_ context.Context, id string) (*models.Task, error) {
	return f.tasks[id], nil
}

func (f *childStallStoreFake) GetTaskSession(_ context.Context, id string) (*models.TaskSession, error) {
	return f.sessions[id], nil
}

func (f *childStallStoreFake) GetPrimarySessionByTaskID(_ context.Context, taskID string) (*models.TaskSession, error) {
	for _, session := range f.sessions {
		if session.TaskID == taskID && session.IsPrimary {
			return session, nil
		}
	}
	return nil, repoerrors.ErrNoPrimarySession
}

func (f *childStallStoreFake) GetLatestTaskStepTransitionID(context.Context, string) (int64, error) {
	return 1, nil
}

func (f *childStallStoreFake) ListPendingInteractions(context.Context, models.PendingInteractionFilter) ([]*models.Message, error) {
	return nil, nil
}

func (f *childStallStoreFake) GetMessage(context.Context, string) (*models.Message, error) {
	return nil, nil
}

// childStallStepGetterFake marks every step as requiring the completion signal,
// so a missing-signal candidate classifies as qualified in these tests.
type childStallStepGetterFake struct{}

func (childStallStepGetterFake) GetStep(_ context.Context, stepID string) (*wfmodels.WorkflowStep, error) {
	return &wfmodels.WorkflowStep{ID: stepID, AutoAdvanceRequiresSignal: true}, nil
}

func (childStallStepGetterFake) GetNextStepByPosition(context.Context, string, int) (*wfmodels.WorkflowStep, error) {
	return nil, nil
}

func (childStallStepGetterFake) GetPreviousStepByPosition(context.Context, string, int) (*wfmodels.WorkflowStep, error) {
	return nil, nil
}

func (childStallStepGetterFake) GetWorkflowMeta(context.Context, string) (WorkflowMeta, error) {
	return WorkflowMeta{}, nil
}

// childStallTestOrchestratorRepo answers the one session read the readiness
// check makes before it can drain, so the reconcile test never needs a full
// task repository.
type childStallTestOrchestratorRepo struct {
	sessionExecutorStore
}

func (childStallTestOrchestratorRepo) GetTaskSession(context.Context, string) (*models.TaskSession, error) {
	return nil, nil
}

func newChildStallReconcileFixture(t *testing.T) (*ChildStallProducer, *messagequeue.Service, *childStallStoreFake) {
	t.Helper()
	log := testLogger()
	queue := messagequeue.NewServiceMemory(log)
	svc := &Service{
		logger:             log,
		messageQueue:       queue,
		repo:               childStallTestOrchestratorRepo{},
		workflowStepGetter: childStallStepGetterFake{},
	}
	store := newChildStallStoreFake()
	producer := NewChildStallProducer(svc, store, log)
	producer.now = func() time.Time { return childStallReconcileNow }
	svc.SetChildStallProducer(producer)
	return producer, queue, store
}

func childStallAlertMetadata(turnID string) map[string]interface{} {
	return map[string]interface{}{
		messagequeue.MetadataChildStallAlert: true,
		messagequeue.MetadataChildStallAlerts: []interface{}{map[string]interface{}{
			"child_task_id": "child",
			"turn_id":       turnID,
			"cause":         "missing_completion_signal",
		}},
		messagequeue.MetadataSenderTaskID: "child",
	}
}

func admitChildStallAlert(
	t *testing.T, ctx context.Context, queue *messagequeue.Service,
	parentTaskID, sessionID, key, turnID string,
) {
	t.Helper()
	identity, err := queue.ResolveSessionIdentity(ctx, parentTaskID, sessionID)
	if err != nil {
		t.Fatalf("resolve session identity: %v", err)
	}
	if _, _, err := queue.QueueMessageWithMetadataForSessionWithClientQueueID(
		ctx, identity, key, "CHILD TASK NEEDS ATTENTION", "", messagequeue.QueuedByServer,
		false, nil, childStallAlertMetadata(turnID), nil,
	); err != nil {
		t.Fatalf("admit child stall alert: %v", err)
	}
}

func admitUserEntry(t *testing.T, ctx context.Context, queue *messagequeue.Service, taskID, sessionID, key string) {
	t.Helper()
	identity, err := queue.ResolveSessionIdentity(ctx, taskID, sessionID)
	if err != nil {
		t.Fatalf("resolve session identity: %v", err)
	}
	if _, _, err := queue.QueueMessageWithMetadataForSessionWithClientQueueID(
		ctx, identity, key, "user message", "", "user-1", false, nil, nil, nil,
	); err != nil {
		t.Fatalf("admit user entry: %v", err)
	}
}

func seedStalledChild(store *childStallStoreFake, parent *models.Task) *models.Turn {
	store.tasks["parent"] = parent
	store.tasks["child"] = &models.Task{
		ID: "child", State: v1.TaskStateInProgress, ParentID: "parent",
		WorkspaceID: "ws", WorkflowStepID: "step-1",
	}
	store.sessions["child-session"] = &models.TaskSession{
		ID: "child-session", TaskID: "child", State: models.TaskSessionStateWaitingForInput,
	}
	turn := &models.Turn{
		ID: "turn-1", TaskID: "child", TaskSessionID: "child-session",
		CompletedAt: timePtrTime(childStallReconcileNow.Add(-time.Minute)),
		Metadata: map[string]interface{}{
			models.TurnMetaKeyChildStallStart: models.ChildStallStart{
				ParentTaskID: "parent", WorkspaceID: "ws", TransitionID: 1,
			}.ToMap(),
		},
	}
	store.turns["turn-1"] = turn
	return turn
}

func timePtrTime(at time.Time) *time.Time { return &at }

func deliveredState(queueID, parentSessionID string) models.ChildStallState {
	return models.ChildStallState{
		State: childStallStateDelivered, Cause: "missing_completion_signal",
		QueueID: queueID, ParentSessionID: parentSessionID,
	}
}

func queueEntryIDs(t *testing.T, ctx context.Context, queue *messagequeue.Service, taskID, sessionID string) []string {
	t.Helper()
	identity, err := queue.ResolveSessionIdentity(ctx, taskID, sessionID)
	if err != nil {
		t.Fatalf("resolve session identity: %v", err)
	}
	entries, _, err := queue.SnapshotSessionForIdentity(ctx, identity)
	if err != nil {
		t.Fatalf("snapshot session: %v", err)
	}
	ids := make([]string, 0, len(entries))
	for i := range entries {
		ids = append(ids, entries[i].ID)
	}
	return ids
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestReconcileDeliveredRehomesToCurrentPrimary(t *testing.T) {
	producer, queue, store := newChildStallReconcileFixture(t)
	ctx := context.Background()
	parent := &models.Task{ID: "parent", State: v1.TaskStateInProgress}
	turn := seedStalledChild(store, parent)
	store.sessions["sess-old"] = &models.TaskSession{
		ID: "sess-old", TaskID: "parent", State: models.TaskSessionStateWaitingForInput,
	}
	store.sessions["sess-new"] = &models.TaskSession{
		ID: "sess-new", TaskID: "parent", IsPrimary: true, State: models.TaskSessionStateWaitingForInput,
	}
	admitChildStallAlert(t, ctx, queue, "parent", "sess-old", "alert-key", "turn-1")

	start, _ := models.LoadChildStallStart(turn.Metadata)
	producer.reconcileDelivered(ctx, turn, start, deliveredState("alert-key", "sess-old"), childStallReconcileNow, &childStallPassStats{})

	oldIDs := queueEntryIDs(t, ctx, queue, "parent", "sess-old")
	newIDs := queueEntryIDs(t, ctx, queue, "parent", "sess-new")
	state, _ := models.LoadChildStallState(turn.Metadata)
	t.Logf("old=%v new=%v state=%+v", oldIDs, newIDs, state)

	if len(oldIDs) != 0 {
		t.Fatalf("old session still holds entries: %v", oldIDs)
	}
	if !containsID(newIDs, "alert-key") {
		t.Fatalf("new primary missing re-homed alert, got %v", newIDs)
	}
	if state.ParentSessionID != "sess-new" {
		t.Fatalf("parent session = %q, want sess-new", state.ParentSessionID)
	}
}

func TestReconcileDeliveredHoldsWithoutPromptablePrimary(t *testing.T) {
	producer, queue, store := newChildStallReconcileFixture(t)
	ctx := context.Background()
	parent := &models.Task{ID: "parent", State: v1.TaskStateInProgress}
	turn := seedStalledChild(store, parent)
	store.sessions["sess-term"] = &models.TaskSession{
		ID: "sess-term", TaskID: "parent", IsPrimary: true, State: models.TaskSessionStateFailed,
	}
	admitChildStallAlert(t, ctx, queue, "parent", "sess-term", "alert-key", "turn-1")

	start, _ := models.LoadChildStallStart(turn.Metadata)
	producer.reconcileDelivered(ctx, turn, start, deliveredState("alert-key", "sess-term"), childStallReconcileNow, &childStallPassStats{})

	if ids := queueEntryIDs(t, ctx, queue, "parent", "sess-term"); len(ids) != 0 {
		t.Fatalf("held notice should be withdrawn from the queue, got %v", ids)
	}
	state, _ := models.LoadChildStallState(turn.Metadata)
	if state.State != childStallStateWaitingParent {
		t.Fatalf("state = %q, want %q", state.State, childStallStateWaitingParent)
	}
	if resolved, _ := turn.Metadata[models.TurnMetaKeyChildStallResolved].(bool); resolved {
		t.Fatal("held candidate must stay scannable, but is marked resolved")
	}
}

func TestReconcileDeliveredWithdrawsWhenParentArchived(t *testing.T) {
	producer, queue, store := newChildStallReconcileFixture(t)
	ctx := context.Background()
	archivedAt := childStallReconcileNow.Add(-time.Hour)
	parent := &models.Task{ID: "parent", State: v1.TaskStateInProgress, ArchivedAt: &archivedAt}
	turn := seedStalledChild(store, parent)
	store.sessions["sess-old"] = &models.TaskSession{
		ID: "sess-old", TaskID: "parent", IsPrimary: true, State: models.TaskSessionStateWaitingForInput,
	}
	admitChildStallAlert(t, ctx, queue, "parent", "sess-old", "alert-key", "turn-1")

	start, _ := models.LoadChildStallStart(turn.Metadata)
	producer.reconcileDelivered(ctx, turn, start, deliveredState("alert-key", "sess-old"), childStallReconcileNow, &childStallPassStats{})

	if ids := queueEntryIDs(t, ctx, queue, "parent", "sess-old"); len(ids) != 0 {
		t.Fatalf("archived parent notice should be withdrawn, got %v", ids)
	}
	if resolved, _ := turn.Metadata[models.TurnMetaKeyChildStallResolved].(bool); !resolved {
		t.Fatal("withdrawn candidate should be resolved")
	}
}

func TestReconcileDeliveredPreservesNonAlertEntries(t *testing.T) {
	producer, queue, store := newChildStallReconcileFixture(t)
	ctx := context.Background()
	archivedAt := childStallReconcileNow.Add(-time.Hour)
	parent := &models.Task{ID: "parent", State: v1.TaskStateInProgress, ArchivedAt: &archivedAt}
	turn := seedStalledChild(store, parent)
	store.sessions["sess-old"] = &models.TaskSession{
		ID: "sess-old", TaskID: "parent", IsPrimary: true, State: models.TaskSessionStateWaitingForInput,
	}
	admitChildStallAlert(t, ctx, queue, "parent", "sess-old", "alert-key", "turn-1")
	admitUserEntry(t, ctx, queue, "parent", "sess-old", "user-key")

	start, _ := models.LoadChildStallStart(turn.Metadata)
	producer.reconcileDelivered(ctx, turn, start, deliveredState("alert-key", "sess-old"), childStallReconcileNow, &childStallPassStats{})

	ids := queueEntryIDs(t, ctx, queue, "parent", "sess-old")
	if containsID(ids, "alert-key") {
		t.Fatalf("alert should be withdrawn, got %v", ids)
	}
	if !containsID(ids, "user-key") {
		t.Fatalf("user entry must be preserved, got %v", ids)
	}
}

func TestRunPassReconcilesDeliveredNoticeToCurrentPrimary(t *testing.T) {
	producer, queue, store := newChildStallReconcileFixture(t)
	ctx := context.Background()
	parent := &models.Task{ID: "parent", State: v1.TaskStateInProgress}
	turn := seedStalledChild(store, parent)
	turn.Metadata[models.TurnMetaKeyChildStall] = deliveredState("alert-key", "sess-old").ToMap()
	store.listed = []*models.Turn{turn}
	store.sessions["sess-old"] = &models.TaskSession{
		ID: "sess-old", TaskID: "parent", State: models.TaskSessionStateWaitingForInput,
	}
	store.sessions["sess-new"] = &models.TaskSession{
		ID: "sess-new", TaskID: "parent", IsPrimary: true, State: models.TaskSessionStateWaitingForInput,
	}
	admitChildStallAlert(t, ctx, queue, "parent", "sess-old", "alert-key", "turn-1")

	producer.runPass(ctx)

	if ids := queueEntryIDs(t, ctx, queue, "parent", "sess-old"); len(ids) != 0 {
		t.Fatalf("runPass left the notice on the old session: %v", ids)
	}
	if ids := queueEntryIDs(t, ctx, queue, "parent", "sess-new"); !containsID(ids, "alert-key") {
		t.Fatalf("runPass did not re-home the notice to the current primary, got %v", ids)
	}
}

func TestPruneChildStallAlertsSupersedesOlderForSameChild(t *testing.T) {
	producer, queue, store := newChildStallReconcileFixture(t)
	ctx := context.Background()
	parent := &models.Task{ID: "parent", State: v1.TaskStateInProgress}
	seedStalledChild(store, parent)
	second := &models.Turn{
		ID: "turn-2", TaskID: "child", TaskSessionID: "child-session",
		CompletedAt: timePtrTime(childStallReconcileNow.Add(-30 * time.Second)),
		Metadata: map[string]interface{}{
			models.TurnMetaKeyChildStallStart: models.ChildStallStart{
				ParentTaskID: "parent", WorkspaceID: "ws", TransitionID: 1,
			}.ToMap(),
		},
	}
	store.turns["turn-2"] = second
	store.sessions["sess-old"] = &models.TaskSession{
		ID: "sess-old", TaskID: "parent", IsPrimary: true, State: models.TaskSessionStateWaitingForInput,
	}
	admitChildStallAlert(t, ctx, queue, "parent", "sess-old", "alert-old", "turn-1")
	admitUserEntry(t, ctx, queue, "parent", "sess-old", "user-key")
	admitChildStallAlert(t, ctx, queue, "parent", "sess-old", "alert-new", "turn-2")

	identity, err := queue.ResolveSessionIdentity(ctx, "parent", "sess-old")
	if err != nil {
		t.Fatalf("resolve session identity: %v", err)
	}
	producer.svc.pruneStaleChildStallAlerts(ctx, identity)

	ids := queueEntryIDs(t, ctx, queue, "parent", "sess-old")
	if containsID(ids, "alert-old") {
		t.Fatalf("older notice for the same child should be superseded, got %v", ids)
	}
	if !containsID(ids, "alert-new") {
		t.Fatalf("newer notice for the same child must be kept, got %v", ids)
	}
	if !containsID(ids, "user-key") {
		t.Fatalf("user entry must be preserved, got %v", ids)
	}
}
