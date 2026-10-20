package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	agentsettingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	"github.com/kandev/kandev/internal/orchestrator/executor"
	"github.com/kandev/kandev/internal/orchestrator/messagequeue"
	"github.com/kandev/kandev/internal/task/models"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

const (
	orphanRouteHeldMessage   = "held follow-up for the orphaned route"
	orphanRouteDurablePrompt = "original prompt before the restart"
)

type orphanRouteFixture struct {
	ctx          context.Context
	taskID       string
	sessionID    string
	repo         *sqliterepo.Repository
	svc          *Service
	agentManager *mockAgentManager
	launches     *int
	launchMu     *sync.Mutex
	// launchPrompts records each fresh launch's initial prompt, which the
	// runtime sends through ACP rather than PromptAgent.
	launchPrompts []string
	// launchAttemptID is the startup attempt that owns the fresh execution;
	// its runtime callbacks carry it.
	launchAttemptID string
}

// newOrphanedRouteActionFixture models the production shape left by the
// restart orphan sweep: a WAITING_FOR_INPUT session whose dynamic route is
// action_required/orphaned_starting_route, with no executors_running row and
// therefore no AgentExecutionID, and one user message held in the queue. The
// route-action handler's claim of the successor generation is then applied
// the way persistDynamicRouteSelection projects it. tryNext selects the next
// candidate; otherwise the same candidate is re-claimed (Retry).
// heldTranscriptRecorded models the chat handler's atomic queued send, which
// writes the held message's transcript row when it queues the entry.
func newOrphanedRouteActionFixture(t *testing.T, tryNext, heldTranscriptRecorded bool) *orphanRouteFixture {
	t.Helper()
	ctx := context.Background()
	f := &orphanRouteFixture{
		ctx:       ctx,
		taskID:    "task-orphan-route-action",
		sessionID: "session-orphan-route-action",
		launchMu:  &sync.Mutex{},
	}
	launches := 0
	f.launches = &launches
	f.repo = setupTestRepo(t)
	seedTaskAndSession(t, f.repo, f.taskID, f.sessionID, models.TaskSessionStateWaitingForInput)
	taskRepo := newMockTaskRepo()
	seedMockTaskState(taskRepo, f.taskID, v1.TaskStateInProgress)
	f.agentManager = &mockAgentManager{
		launchAgentFunc: func(launchCtx context.Context, req *executor.LaunchAgentRequest) (*executor.LaunchAgentResponse, error) {
			f.launchMu.Lock()
			launches++
			f.launchAttemptID = executor.ResumeAttemptIDFromContext(launchCtx)
			f.launchPrompts = append(f.launchPrompts, req.TaskDescription)
			f.launchMu.Unlock()
			// The lifecycle manager persists the launched execution's
			// executors_running row, which later prompts resume through.
			if err := f.repo.UpsertExecutorRunning(launchCtx, &models.ExecutorRunning{
				ID: req.SessionID, SessionID: req.SessionID, TaskID: req.TaskID,
				AgentExecutionID: "fresh-execution-" + req.SessionID, Status: "ready",
			}); err != nil {
				return nil, err
			}
			return &executor.LaunchAgentResponse{AgentExecutionID: "fresh-execution-" + req.SessionID}, nil
		},
		// The started runtime admits and dispatches its initial prompt, which
		// releases the startup owner's hold on the session queue.
		startAgentProcessFunc: func(ctx context.Context, _ string) error {
			f.agentManager.mu.Lock()
			admit, dispatched := f.agentManager.initialPromptAdmissionCallback, f.agentManager.initialPromptDispatchCallback
			f.agentManager.mu.Unlock()
			if admit != nil {
				f.launchMu.Lock()
				prompt := ""
				if len(f.launchPrompts) > 0 {
					prompt = f.launchPrompts[len(f.launchPrompts)-1]
				}
				f.launchMu.Unlock()
				if err := admit(ctx, prompt, nil); err != nil {
					return err
				}
			}
			if dispatched != nil {
				dispatched()
			}
			return nil
		},
		isAgentRunningFn: func(context.Context, string) bool { return true },
		isAgentReadyFn:   func(context.Context, string) bool { return true },
	}
	f.svc = createTestServiceWithScheduler(f.repo, newMockStepGetter(), taskRepo, f.agentManager)
	f.svc.turnService = &repoTurnService{repo: f.repo}

	engine := dynamicruntime.NewEngine(dynamicruntime.WithPersistence(f.repo))
	resolver := agentruntime.NewProfileExecutionResolver(newOrphanRouteProfileStore(t), engine, true)
	f.svc.SetProfileExecutionResolver(resolver)
	profile := dynamicruntime.Profile{
		ID: "dynamic-logical", Version: 1,
		Candidates: []dynamicruntime.Candidate{
			{ID: "candidate-1", Enabled: true},
			{ID: "candidate-2", Enabled: true},
		},
	}
	initial, err := engine.Select(f.sessionID, profile, 0, "")
	if err != nil {
		t.Fatalf("claim initial route generation: %v", err)
	}
	f.projectRoute(t, profile.ID, initial)
	f.svc.markDynamicRouteActionRequired(ctx, f.sessionID, initial.Generation, dynamicRouteOrphanedStartingReasonForTest)
	if session := f.session(t); session.RouteState != dynamicRouteStatusActionRequired {
		t.Fatalf("seeded route state = %q, want action_required", session.RouteState)
	}

	action := "retry"
	if tryNext {
		action = "try_next"
	}
	claimed, err := resolver.ResolveRouteAction(
		ctx, f.sessionID, profile.ID, initial.ExecutionProfileID, initial.Generation, action,
	)
	if err != nil {
		t.Fatalf("claim successor route generation with %s: %v", action, err)
	}
	f.projectRoute(t, profile.ID, claimed.Decision)
	if session := f.session(t); tryNext == (session.ExecutionProfileID == initial.ExecutionProfileID) {
		t.Fatalf("%s claimed candidate %q from %q", action, session.ExecutionProfileID, initial.ExecutionProfileID)
	}

	turnStarted := time.Now().UTC().Add(-2 * time.Minute)
	turnCompleted := turnStarted.Add(time.Second)
	if err := f.repo.CreateTurn(ctx, &models.Turn{
		ID: "orphan-route-turn", TaskSessionID: f.sessionID, TaskID: f.taskID,
		StartedAt: turnStarted, CompletedAt: &turnCompleted,
	}); err != nil {
		t.Fatalf("seed completed turn: %v", err)
	}
	if err := f.repo.CreateMessage(ctx, &models.Message{
		ID: "orphan-route-user-message", TaskSessionID: f.sessionID, TaskID: f.taskID, TurnID: "orphan-route-turn",
		AuthorType: models.MessageAuthorUser, Content: orphanRouteDurablePrompt,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed durable user message: %v", err)
	}
	var heldMetadata map[string]interface{}
	if heldTranscriptRecorded {
		const heldTranscriptID = "orphan-route-held-transcript"
		heldMetadata = map[string]interface{}{
			metaKeyUserMessageRecorded:                      true,
			messagequeue.MetadataDurableTranscriptMessageID: heldTranscriptID,
		}
		if err := f.repo.CreateMessage(ctx, &models.Message{
			ID: heldTranscriptID, TaskSessionID: f.sessionID, TaskID: f.taskID, TurnID: "orphan-route-turn",
			AuthorType: models.MessageAuthorUser, Content: orphanRouteHeldMessage, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("seed held transcript message: %v", err)
		}
	}
	if _, err := f.svc.messageQueue.QueueMessageWithMetadata(
		ctx, f.sessionID, f.taskID, orphanRouteHeldMessage, "", messagequeue.QueuedByUser, false, nil, heldMetadata,
	); err != nil {
		t.Fatalf("queue held message: %v", err)
	}
	if _, err := f.repo.GetExecutorRunningBySessionID(ctx, f.sessionID); !errors.Is(err, models.ErrExecutorRunningNotFound) {
		t.Fatalf("fixture must have no executors_running row, lookup err = %v", err)
	}
	return f
}

// newOrphanRouteProfileStore stores the dynamic logical profile and its two
// concrete candidates so route actions and successor launches resolve them.
func newOrphanRouteProfileStore(t *testing.T) agentsettingsstore.Repository {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo, cleanup, err := agentsettingsstore.Provide(db, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup() })
	ctx := context.Background()
	for _, agent := range []*agentsettingsmodels.Agent{
		{ID: "dynamic", Name: "dynamic"},
		{ID: "concrete-agent", Name: "concrete-agent"},
	} {
		if err := repo.CreateAgent(ctx, agent); err != nil {
			t.Fatal(err)
		}
	}
	profiles := []*agentsettingsmodels.AgentProfile{
		{ID: "dynamic-logical", AgentID: "dynamic", Name: "Cascade", Enabled: true},
		{ID: "candidate-1", AgentID: "concrete-agent", Name: "candidate-1", Enabled: true},
		{ID: "candidate-2", AgentID: "concrete-agent", Name: "candidate-2", Enabled: true},
	}
	for _, profile := range profiles {
		if err := repo.CreateAgentProfile(ctx, profile); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.CreateDynamicAgentProfile(ctx,
		&agentsettingsmodels.DynamicAgentProfile{ProfileID: "dynamic-logical", Version: 1},
		[]agentsettingsmodels.DynamicAgentRoute{
			{DynamicProfileID: "dynamic-logical", Position: 0, ExecutionProfileID: "candidate-1", Enabled: true},
			{DynamicProfileID: "dynamic-logical", Position: 1, ExecutionProfileID: "candidate-2", Enabled: true},
		},
	); err != nil {
		t.Fatal(err)
	}
	return repo
}

// dynamicRouteOrphanedStartingReasonForTest is the reason the restart orphan
// sweep records (reconcileOrphanedDynamicStartingRoute).
const dynamicRouteOrphanedStartingReasonForTest = "orphaned_starting_route"

func (f *orphanRouteFixture) projectRoute(t *testing.T, logicalProfileID string, decision dynamicruntime.RouteDecision) {
	t.Helper()
	session := f.session(t)
	session.AgentProfileID = logicalProfileID
	session.ExecutionProfileID = decision.ExecutionProfileID
	session.RouteGeneration = decision.Generation
	session.RouteState = decision.Status
	if session.RouteState == "" {
		session.RouteState = "starting"
	}
	session.AgentExecutionID = ""
	if err := f.repo.UpdateTaskSession(f.ctx, session); err != nil {
		t.Fatalf("project route decision: %v", err)
	}
}

func (f *orphanRouteFixture) session(t *testing.T) *models.TaskSession {
	t.Helper()
	session, err := f.repo.GetTaskSession(f.ctx, f.sessionID)
	if err != nil || session == nil {
		t.Fatalf("load session: session=%#v err=%v", session, err)
	}
	return session
}

func (f *orphanRouteFixture) launchCount() int {
	f.launchMu.Lock()
	defer f.launchMu.Unlock()
	return *f.launches
}

func (f *orphanRouteFixture) heldMessageDeliveries() int {
	return len(f.deliveriesOf(orphanRouteHeldMessage))
}

// deliveriesOf lists every prompt sent to an agent, at launch or later, that
// carries text.
func (f *orphanRouteFixture) deliveriesOf(text string) []string {
	f.launchMu.Lock()
	sent := append([]string(nil), f.launchPrompts...)
	f.launchMu.Unlock()
	f.agentManager.mu.Lock()
	sent = append(sent, f.agentManager.capturedPrompts...)
	f.agentManager.mu.Unlock()
	matched := []string{}
	for _, prompt := range sent {
		for range strings.Count(prompt, text) {
			matched = append(matched, prompt)
		}
	}
	return matched
}

// TestLaunchDynamicRouteAction_OrphanedRouteWithoutExecutorLaunchesAndDeliversHeldMessageOnce
// pins AC 001.27 at the relaunch boundary. Retry and Try next launch one fresh
// execution for the claimed candidate. The launch replays the latest user
// prompt the queue does not own, so the held message appears in no launch
// prompt (neither as the replayed prompt nor in the continuation), and its
// queue entry stays queued, unchanged, for the ordinary post-turn drain to
// deliver once.
func TestLaunchDynamicRouteAction_OrphanedRouteWithoutExecutorLaunchesAndDeliversHeldMessageOnce(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tryNext  bool
		recorded bool
	}{
		{name: "retry", tryNext: false},
		{name: "try next", tryNext: true},
		{name: "retry with held transcript recorded", tryNext: false, recorded: true},
		{name: "try next with held transcript recorded", tryNext: true, recorded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOrphanedRouteActionFixture(t, tc.tryNext, tc.recorded)
			claimed := f.session(t)
			before := f.svc.messageQueue.GetStatus(f.ctx, f.sessionID)
			if before.Count != 1 {
				t.Fatalf("fixture queue holds %d entries, want 1", before.Count)
			}

			if err := f.svc.LaunchDynamicRouteAction(f.ctx, f.sessionID); err != nil {
				t.Fatalf("LaunchDynamicRouteAction on an orphaned route without an executor row: %v", err)
			}

			if got := f.launchCount(); got != 1 {
				t.Fatalf("fresh launches = %d, want 1", got)
			}
			session := f.session(t)
			if session.RouteState == dynamicRouteStatusActionRequired {
				t.Fatalf("route stayed action_required (reason %q) after a successful launch", session.RouteReason)
			}
			if session.RouteGeneration != claimed.RouteGeneration || session.ExecutionProfileID != claimed.ExecutionProfileID {
				t.Fatalf("launch changed the claimed route: generation %d/%d profile %q/%q",
					session.RouteGeneration, claimed.RouteGeneration, session.ExecutionProfileID, claimed.ExecutionProfileID)
			}
			if got := len(f.deliveriesOf(orphanRouteDurablePrompt)); got == 0 {
				t.Fatal("fresh launch did not replay the latest user prompt the queue does not own")
			}
			if got := f.heldMessageDeliveries(); got != 0 {
				t.Fatalf("held queued message sent %d times by the relaunch itself, want 0 so the queue delivers it once (deliveries: %q)",
					got, f.deliveriesOf(orphanRouteHeldMessage))
			}
			after := f.svc.messageQueue.GetStatus(f.ctx, f.sessionID)
			if after.Count != 1 || after.Entries[0].ID != before.Entries[0].ID ||
				after.Entries[0].Content != orphanRouteHeldMessage {
				t.Fatalf("held queue entry after relaunch = %+v, want the original entry still queued", after.Entries)
			}
		})
	}
}

func TestStopDynamicRelaunchPredecessor_ClassifiesStopOutcomes(t *testing.T) {
	genuine := errors.New("runtime teardown failed")
	for _, tc := range []struct {
		name        string
		executionID string
		stopErr     error
		want        bool
		wantStops   int
	}{
		{name: "empty predecessor id is already gone", executionID: "", want: true, wantStops: 0},
		{name: "runtime not found is already gone", executionID: "exec-1", stopErr: agentruntime.ErrNotFound, want: true, wantStops: 1},
		{name: "genuine stop error still fails", executionID: "exec-1", stopErr: genuine, want: false, wantStops: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := setupTestRepo(t)
			agentManager := &mockAgentManager{stopAgentWithReasonErr: tc.stopErr}
			svc := createTestServiceWithScheduler(repo, newMockStepGetter(), newMockTaskRepo(), agentManager)

			if got := svc.stopDynamicRelaunchPredecessor(context.Background(), tc.executionID); got != tc.want {
				t.Fatalf("stopDynamicRelaunchPredecessor(%q) = %v, want %v", tc.executionID, got, tc.want)
			}
			if got := len(agentManager.stopAgentWithReasonArgs); got != tc.wantStops {
				t.Fatalf("runtime stop calls = %d, want %d", got, tc.wantStops)
			}
		})
	}
}
