package dashboard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/auth/authn"
	"github.com/kandev/kandev/internal/office/dashboard"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

type stubScopeSource struct{ scope string }

func (s stubScopeSource) OfficeOverviewScope(context.Context) (string, error) { return s.scope, nil }

// countingOverviewReader counts snapshot builds and can hold them open so a
// test can pile concurrent callers onto one build.
type countingOverviewReader struct {
	*sqlite.Repository
	builds atomic.Int32
	delay  time.Duration
}

func (r *countingOverviewReader) ListOverviewOpenTasks(ctx context.Context, ids []string) ([]*sqlite.OverviewTaskRow, error) {
	r.builds.Add(1)
	time.Sleep(r.delay)
	return r.Repository.ListOverviewOpenTasks(ctx, ids)
}

// overviewFixture seeds an Office workspace, a non-Office workspace, and a
// workspace the caller cannot see. The message table deliberately has no
// content or metadata column: an overview read that touched message bodies
// would fail.
func overviewFixture(t *testing.T) *testDeps {
	t.Helper()
	deps := newTestDeps(t)
	mustExec(t, deps, `ALTER TABLE task_sessions ADD COLUMN error_message TEXT DEFAULT ''`)
	mustExec(t, deps, `CREATE TABLE task_session_messages (
		id TEXT PRIMARY KEY, task_session_id TEXT NOT NULL, author_type TEXT NOT NULL, created_at TIMESTAMP NOT NULL)`)
	mustExec(t, deps, `CREATE TABLE queued_messages (
		id TEXT PRIMARY KEY, session_id TEXT NOT NULL, task_id TEXT NOT NULL, position INTEGER NOT NULL,
		content TEXT NOT NULL DEFAULT '', queued_at TIMESTAMP NOT NULL, queued_by TEXT NOT NULL DEFAULT '')`)
	mustExec(t, deps, `CREATE TABLE IF NOT EXISTS task_step_transitions (
		id TEXT PRIMARY KEY, task_id TEXT NOT NULL, occurred_at TIMESTAMP NOT NULL)`)

	now := time.Now().UTC()
	insertOverviewTask(t, deps, "t-parent", "ws-office", "IN_PROGRESS", "", now)
	insertOverviewTask(t, deps, "t-run", "ws-office", "IN_PROGRESS", "t-parent", now)
	insertOverviewTask(t, deps, "t-done", "ws-office", "COMPLETED", "", now)
	insertOverviewTask(t, deps, "t-fail", "ws-kanban", "IN_PROGRESS", "", now)
	insertOverviewTask(t, deps, "t-hidden", "ws-hidden", "IN_PROGRESS", "", now)
	mustExec(t, deps, `INSERT INTO agents (id, name, created_at, updated_at) VALUES ('agent-a', 'claude-code', ?, ?)`, now, now)
	mustExec(t, deps, `INSERT INTO agent_profiles (id, agent_id, name, agent_display_name, created_at, updated_at)
		VALUES ('p1', 'agent-a', 'fast', 'Agent A', ?, ?)`, now, now)
	insertOverviewSession(t, deps, "s-run", "t-run", "RUNNING", "", now.Add(-40*time.Minute))
	insertOverviewSession(t, deps, "s-fail", "t-fail", "FAILED", "AI_APICallError: Rate limit exceeded\ntrace", now.Add(-5*time.Minute))
	insertOverviewSession(t, deps, "s-hidden", "t-hidden", "RUNNING", "", now.Add(-time.Minute))
	mustExec(t, deps, `INSERT INTO task_session_messages (id, task_session_id, author_type, created_at)
		VALUES ('m1', 's-run', 'agent', ?), ('m2', 's-run', 'user', ?)`, now.Add(-20*time.Minute), now.Add(-time.Minute))
	mustExec(t, deps, `INSERT INTO queued_messages (id, session_id, task_id, position, content, queued_at, queued_by)
		VALUES ('q1', 's-fail', 't-fail', 1, '<kandev-system>x</kandev-system>
retry the deploy', ?, 'user')`, now.Add(-30*time.Minute))
	return deps
}

func mustExec(t *testing.T, deps *testDeps, query string, args ...interface{}) {
	t.Helper()
	if _, err := deps.db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func insertOverviewTask(t *testing.T, deps *testDeps, id, wsID, state, parentID string, at time.Time) {
	t.Helper()
	mustExec(t, deps, `INSERT INTO tasks (id, workspace_id, title, state, parent_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, wsID, "title "+id, state, parentID, at, at)
}

func insertOverviewSession(t *testing.T, deps *testDeps, id, taskID, state, errMsg string, started time.Time) {
	t.Helper()
	mustExec(t, deps, `INSERT INTO task_sessions (id, task_id, agent_profile_id, state, error_message, started_at, updated_at)
		VALUES (?, ?, 'p1', ?, ?, ?, ?)`, id, taskID, state, errMsg, started, started)
}

func overviewLister() *stubWorkspaceLister {
	return &stubWorkspaceLister{workspaces: []*taskmodels.Workspace{
		{ID: "ws-office", Name: "Office WS", OfficeWorkflowID: "wf"},
		{ID: "ws-kanban", Name: "Board WS"},
	}}
}

func TestOverviewScopeSelectsWorkspaces(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if resp.Scope != dashboard.OverviewScopeOffice || len(resp.Workspaces) != 1 || resp.Workspaces[0].WorkspaceID != "ws-office" {
		t.Fatalf("default scope = %q workspaces = %+v; want office scope with ws-office only", resp.Scope, resp.Workspaces)
	}

	deps.svc.SetOverviewScopeSource(stubScopeSource{scope: dashboard.OverviewScopeReachable})
	resp, err = deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if resp.Scope != dashboard.OverviewScopeReachable || len(resp.Workspaces) != 2 {
		t.Fatalf("reachable scope = %q workspaces = %+v; want both reachable workspaces", resp.Scope, resp.Workspaces)
	}
	for _, w := range resp.Workspaces {
		if w.WorkspaceID == "ws-hidden" {
			t.Fatal("a workspace outside the caller's list must never appear")
		}
	}
}

func TestOverviewMetricsAndSections(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)
	deps.svc.SetOverviewScopeSource(stubScopeSource{scope: dashboard.OverviewScopeReachable})
	deps.svc.SetOverviewSessionLimit(8)

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	byID := map[string]dashboard.WorkspaceAggregateEntry{}
	for _, w := range resp.Workspaces {
		byID[w.WorkspaceID] = w
	}
	office := byID["ws-office"].Metrics
	if office == nil || office.RunningSessions != 1 || office.Problems.Stalled != 1 || office.Completed24h != 1 {
		t.Fatalf("office metrics = %+v; want 1 running session, 1 stalled, 1 completed", office)
	}
	if office.LastOutputTaskID != "t-run" || office.TopWarning == nil || office.TopWarning.TaskID != "t-run" {
		t.Fatalf("office last output / warning = %+v", office)
	}
	if parents := byID["ws-office"].Parents; len(parents) != 1 || parents[0].TaskID != "t-parent" || parents[0].OpenChildren != 1 {
		t.Fatalf("parents = %+v; want t-parent with one open child", parents)
	}
	board := byID["ws-kanban"].Metrics
	if board == nil || board.Problems.Error != 1 || board.QueuedMessages != 1 || board.TopWarning.Reason.Detail != "AI_APICallError: Rate limit exceeded" {
		t.Fatalf("board metrics = %+v; want one error with the verbatim agent error and one queued message", board)
	}
	if byID["ws-kanban"].IsOffice || !byID["ws-office"].IsOffice {
		t.Fatal("is_office must follow the Office workflow id")
	}
	sys := resp.System
	if sys == nil || sys.RunningSessions != 1 || sys.SessionLimit != 8 || sys.UndeliverableMessages != 1 || sys.Problems != 2 {
		t.Fatalf("system = %+v", sys)
	}
	if len(resp.Models) != 1 || resp.Models[0].AgentName != "claude-code" || resp.Models[0].Failed24h != 1 ||
		len(resp.Models[0].Errors) != 1 {
		t.Fatalf("models = %+v; want profile p1 with one failure kind", resp.Models)
	}
	kinds := map[string]bool{}
	for _, ev := range resp.Last24h {
		kinds[ev.Kind] = true
	}
	if !kinds["task_completed"] || !kinds["session_failed"] {
		t.Fatalf("last 24h = %+v; want a completion and a session failure", resp.Last24h)
	}
}

func TestOverviewCacheIsPerCallerAndShort(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	first, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	insertOverviewTask(t, deps, "t-new", "ws-office", "IN_PROGRESS", "", time.Now().UTC())
	second, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if first != second {
		t.Fatal("a repeat request inside the cache window must be served from memory")
	}
	other := authn.WithIdentity(context.Background(), authn.Identity{UserID: "someone-else"})
	third, err := deps.svc.GetWorkspacesAggregate(other)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if third == first || third.Workspaces[0].Metrics.OpenTasks != first.Workspaces[0].Metrics.OpenTasks+1 {
		t.Fatal("another caller must get its own snapshot")
	}
}

func TestOverviewConcurrentMissesShareOneBuild(t *testing.T) {
	deps := overviewFixture(t)
	reader := &countingOverviewReader{Repository: deps.repo, delay: 50 * time.Millisecond}
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(reader)

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := deps.svc.GetWorkspacesAggregate(context.Background()); err != nil {
				t.Errorf("aggregate: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := reader.builds.Load(); got != 1 {
		t.Fatalf("builds = %d, want 1", got)
	}
}

func TestOverviewLists(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)
	deps.svc.SetOverviewScopeSource(stubScopeSource{scope: dashboard.OverviewScopeReachable})
	ctx := context.Background()

	tasks, err := deps.svc.GetOverviewWorkspaceTasks(ctx, "ws-office", "", 0)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if tasks.Filter != dashboard.OverviewFilterProblems || tasks.Total != 1 || tasks.Tasks[0].TaskID != "t-run" ||
		tasks.Tasks[0].SessionID != "s-run" || tasks.Tasks[0].ModelName == "" {
		t.Fatalf("problem list = %+v", tasks)
	}
	if _, err := deps.svc.GetOverviewWorkspaceTasks(ctx, "ws-hidden", "all", 0); !errors.Is(err, dashboard.ErrOverviewWorkspaceNotFound) {
		t.Fatalf("hidden workspace err = %v, want not found", err)
	}
	if _, err := deps.svc.GetOverviewWorkspaceTasks(ctx, "ws-office", "bogus", 0); !errors.Is(err, dashboard.ErrOverviewBadRequest) {
		t.Fatalf("bad filter err = %v, want bad request", err)
	}
	done, err := deps.svc.GetOverviewWorkspaceTasks(ctx, "ws-office", dashboard.OverviewFilterCompleted, 0)
	if err != nil || done.Total != 1 || done.Tasks[0].TaskID != "t-done" {
		t.Fatalf("completed list = %+v err = %v", done, err)
	}

	sessions, err := deps.svc.GetOverviewRunning(ctx, dashboard.OverviewKindSessions, 0)
	if err != nil || sessions.Total != 1 || sessions.Sessions[0].SessionID != "s-run" ||
		sessions.Sessions[0].Status != dashboard.OverviewStatusStalled {
		t.Fatalf("running sessions = %+v err = %v", sessions, err)
	}
	queue, err := deps.svc.GetOverviewRunning(ctx, dashboard.OverviewKindQueue, 0)
	if err != nil || queue.Total != 1 || queue.Queue[0].Status != dashboard.OverviewQueueUndeliverable ||
		queue.Queue[0].FirstLine != "retry the deploy" || queue.Queue[0].Sender != "user" {
		t.Fatalf("queue = %+v err = %v", queue, err)
	}
	running, err := deps.svc.GetOverviewRunning(ctx, dashboard.OverviewKindTasks, 1)
	if err != nil || running.Total != 3 || len(running.Tasks) != 1 {
		t.Fatalf("running tasks = %+v err = %v; want 3 total clipped to 1", running, err)
	}
}

func TestOverviewListRoutes(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)
	cases := map[string]int{
		"/api/v1/office/workspaces/aggregate/tasks?workspace_id=ws-office&filter=all": http.StatusOK,
		"/api/v1/office/workspaces/aggregate/tasks?workspace_id=ws-kanban":            http.StatusNotFound,
		"/api/v1/office/workspaces/aggregate/tasks?workspace_id=ws-office&filter=x":   http.StatusBadRequest,
		"/api/v1/office/workspaces/aggregate/running?kind=sessions":                   http.StatusOK,
		"/api/v1/office/workspaces/aggregate/running?kind=x":                          http.StatusBadRequest,
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		deps.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Fatalf("GET %s = %d, want %d (%s)", path, rec.Code, want, rec.Body.String())
		}
	}
}

func TestOverviewListsUnavailableWithoutReader(t *testing.T) {
	deps := newTestDeps(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	if _, err := deps.svc.GetOverviewRunning(context.Background(), dashboard.OverviewKindTasks, 0); !errors.Is(err, dashboard.ErrWorkspaceAggregateUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
}
