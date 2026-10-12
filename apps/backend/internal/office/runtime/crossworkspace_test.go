package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/office/agents"
	"github.com/kandev/kandev/internal/office/crossworkspace"
	"github.com/kandev/kandev/internal/office/models"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// fakeCrossWorkspaceGateway answers every target with err (when set) or a
// canned body, and records the run context and message it saw.
type fakeCrossWorkspaceGateway struct {
	err     error
	body    json.RawMessage
	runCtx  RunContext
	message CrossWorkspaceMessage
	query   ConversationQuery
}

func (f *fakeCrossWorkspaceGateway) Directory(_ context.Context, runCtx RunContext) (*crossworkspace.Directory, error) {
	f.runCtx = runCtx
	if f.err != nil {
		return nil, f.err
	}
	return &crossworkspace.Directory{Enabled: true, Workspaces: []crossworkspace.ReachableWorkspace{{ID: runCtx.WorkspaceID, Self: true}}}, nil
}

func (f *fakeCrossWorkspaceGateway) Coordinator(_ context.Context, runCtx RunContext, ws string) (*crossworkspace.Coordinator, error) {
	f.runCtx = runCtx
	if f.err != nil {
		return nil, f.err
	}
	return &crossworkspace.Coordinator{WorkspaceID: ws, TaskID: "ctl-1", Source: "setting"}, nil
}

func (f *fakeCrossWorkspaceGateway) raw(runCtx RunContext) (json.RawMessage, error) {
	f.runCtx = runCtx
	if f.err != nil {
		return nil, f.err
	}
	return f.body, nil
}

func (f *fakeCrossWorkspaceGateway) ListTasks(_ context.Context, runCtx RunContext, _, _ string) (json.RawMessage, error) {
	return f.raw(runCtx)
}

func (f *fakeCrossWorkspaceGateway) TaskSessions(_ context.Context, runCtx RunContext, _ string) (json.RawMessage, error) {
	return f.raw(runCtx)
}

func (f *fakeCrossWorkspaceGateway) TaskConversation(_ context.Context, runCtx RunContext, _ string, q ConversationQuery) (json.RawMessage, error) {
	f.query = q
	return f.raw(runCtx)
}

func (f *fakeCrossWorkspaceGateway) TaskPlan(_ context.Context, runCtx RunContext, _ string) (json.RawMessage, error) {
	return f.raw(runCtx)
}

func (f *fakeCrossWorkspaceGateway) SendMessage(_ context.Context, runCtx RunContext, m CrossWorkspaceMessage) (json.RawMessage, error) {
	f.message = m
	return f.raw(runCtx)
}

type crossWorkspaceHarness struct {
	router *gin.Engine
	token  string
}

func newCrossWorkspaceHarness(t *testing.T, taskID string, caps Capabilities, gateway CrossWorkspaceGateway) *crossWorkspaceHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, _, err := settingsstore.Provide(db, db, nil); err != nil {
		t.Fatalf("settings store init: %v", err)
	}
	repo, err := sqlite.NewWithDB(db, db, nil)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	agentSvc := agents.NewAgentService(repo, logger.Default(), nil)
	agentSvc.SetAuth(agents.NewAgentAuth("cross-workspace-test-key"))
	if err := repo.CreateAgentInstance(context.Background(), &models.AgentInstance{
		ID: "agent-1", WorkspaceID: "office", Name: "Coordinator", Role: models.AgentRoleCEO,
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	capabilityJSON, err := MarshalCapabilities(caps)
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	sessionID := ""
	if taskID != "" {
		sessionID = "sess-1"
	}
	token, err := agentSvc.MintRuntimeJWT("agent-1", taskID, "office", "run-1", sessionID, capabilityJSON)
	if err != nil {
		t.Fatalf("mint runtime token: %v", err)
	}
	handler := NewHandler(agentSvc, NewActions(ActionDependencies{Agents: agentSvc}), nil, nil, nil, nil, nil)
	if gateway != nil {
		handler.SetCrossWorkspace(gateway)
	}
	router := gin.New()
	RegisterRoutes(router.Group(""), handler)
	return &crossWorkspaceHarness{router: router, token: token}
}

func (h *crossWorkspaceHarness) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	h.router.ServeHTTP(resp, req)
	return resp
}

var readCaps = Capabilities{CanListTasks: true, CanPostComments: true}

func TestCrossWorkspaceRoutes_RefusalsMapToStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"setting off", crossworkspace.ErrDisabled, http.StatusForbidden},
		{"out of reach", crossworkspace.ErrNotReachable, http.StatusNotFound},
		{"target error", &CrossWorkspaceError{Status: http.StatusBadRequest, Message: "bad"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newCrossWorkspaceHarness(t, "task-1", readCaps, &fakeCrossWorkspaceGateway{err: tc.err})
			for _, path := range []string{
				"/runtime/workspaces/ws-b/tasks",
				"/runtime/tasks/t-b/sessions",
				"/runtime/tasks/t-b/conversation",
				"/runtime/tasks/t-b/plan",
			} {
				if resp := h.do(t, http.MethodGet, path, ""); resp.Code != tc.want {
					t.Fatalf("%s: status = %d, want %d (%s)", path, resp.Code, tc.want, resp.Body.String())
				}
			}
		})
	}
}

func TestCrossWorkspaceRoutes_PassThroughTargetJSON(t *testing.T) {
	gateway := &fakeCrossWorkspaceGateway{body: json.RawMessage(`{"tasks":[{"id":"t-b"}],"total":1}`)}
	h := newCrossWorkspaceHarness(t, "task-1", readCaps, gateway)
	resp := h.do(t, http.MethodGet, "/runtime/workspaces/ws-b/tasks", "")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"t-b"`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if gateway.runCtx.WorkspaceID != "office" || gateway.runCtx.TaskID != "task-1" {
		t.Fatalf("gateway saw run context %+v, want the token's claims", gateway.runCtx)
	}
	resp = h.do(t, http.MethodGet, "/runtime/tasks/t-b/conversation?limit=5&sort=desc", "")
	if resp.Code != http.StatusOK || gateway.query.Limit != 5 || gateway.query.Sort != "desc" {
		t.Fatalf("conversation status=%d query=%+v", resp.Code, gateway.query)
	}
	if resp := h.do(t, http.MethodGet, "/runtime/tasks/t-b/conversation?sort=up", ""); resp.Code != http.StatusBadRequest {
		t.Fatalf("bad sort: status=%d", resp.Code)
	}
}

func TestCrossWorkspaceRoutes_DirectoryAndCoordinator(t *testing.T) {
	h := newCrossWorkspaceHarness(t, "", readCaps, &fakeCrossWorkspaceGateway{})
	if resp := h.do(t, http.MethodGet, "/runtime/workspaces", ""); resp.Code != http.StatusOK ||
		!strings.Contains(resp.Body.String(), `"cross_workspace_access_enabled":true`) {
		t.Fatalf("directory status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := h.do(t, http.MethodGet, "/runtime/workspaces/ws-b/coordinator", ""); resp.Code != http.StatusOK ||
		!strings.Contains(resp.Body.String(), `"ctl-1"`) {
		t.Fatalf("coordinator status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestCrossWorkspaceRoutes_SendMessage(t *testing.T) {
	gateway := &fakeCrossWorkspaceGateway{body: json.RawMessage(`{"status":"queued"}`)}
	h := newCrossWorkspaceHarness(t, "task-1", readCaps, gateway)
	resp := h.do(t, http.MethodPost, "/runtime/tasks/ctl-b/messages", `{"prompt":"please pause W-12","operation_id":"op-1"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if gateway.message.TaskID != "ctl-b" || gateway.message.Prompt != "please pause W-12" || gateway.message.OperationID != "op-1" {
		t.Fatalf("message = %+v", gateway.message)
	}
	if resp := h.do(t, http.MethodPost, "/runtime/tasks/ctl-b/messages", `{"prompt":"x","priority":"high"}`); resp.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status=%d, want 400", resp.Code)
	}
}

func TestCrossWorkspaceRoutes_SendRequiresTaskScopedRun(t *testing.T) {
	h := newCrossWorkspaceHarness(t, "", readCaps, &fakeCrossWorkspaceGateway{body: json.RawMessage(`{}`)})
	if resp := h.do(t, http.MethodPost, "/runtime/tasks/ctl-b/messages", `{"prompt":"hi"}`); resp.Code != http.StatusBadRequest {
		t.Fatalf("taskless send: status=%d, want 400", resp.Code)
	}
}

func TestCrossWorkspaceRoutes_CapabilityAndWiring(t *testing.T) {
	h := newCrossWorkspaceHarness(t, "task-1", Capabilities{}, &fakeCrossWorkspaceGateway{})
	if resp := h.do(t, http.MethodGet, "/runtime/workspaces", ""); resp.Code != http.StatusForbidden {
		t.Fatalf("no list_tasks capability: status=%d, want 403", resp.Code)
	}
	if resp := h.do(t, http.MethodPost, "/runtime/tasks/x/messages", `{"prompt":"hi"}`); resp.Code != http.StatusForbidden {
		t.Fatalf("no post_comment capability: status=%d, want 403", resp.Code)
	}
	unwired := newCrossWorkspaceHarness(t, "task-1", readCaps, nil)
	if resp := unwired.do(t, http.MethodGet, "/runtime/workspaces", ""); resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("no gateway: status=%d, want 503", resp.Code)
	}
}
