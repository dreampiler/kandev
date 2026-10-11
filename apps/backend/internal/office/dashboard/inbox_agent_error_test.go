package dashboard_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/office/dashboard"
	"github.com/kandev/kandev/internal/office/models"
	officeservice "github.com/kandev/kandev/internal/office/service"
)

type stubMarkFixedHandler struct {
	agentErrorCalls []string
	agentErrorErr   error
}

func (s *stubMarkFixedHandler) MarkAgentRunFailedFixed(context.Context, string, string) error {
	return nil
}

func (s *stubMarkFixedHandler) MarkAgentPausedFixed(context.Context, string, string) error {
	return nil
}

func (s *stubMarkFixedHandler) MarkAgentErrorFixed(_ context.Context, _, entryID string) error {
	s.agentErrorCalls = append(s.agentErrorCalls, entryID)
	return s.agentErrorErr
}

func seedInboxAgentError(t *testing.T, deps *testDeps, entryID, agentID, runID, createdAt string) {
	t.Helper()
	mustExec(t, deps, `
		INSERT INTO office_activity_log
			(id, workspace_id, actor_type, actor_id, action, target_type, target_id, details, run_id, session_id, created_at)
		VALUES (?, 'ws1', 'system', 'scheduler', 'agent.error', 'agent', ?, '{}', ?, '', ?)`,
		entryID, agentID, runID, createdAt)
}

func postDismiss(t *testing.T, deps *testDeps, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/office/inbox/dismiss", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)
	return w
}

func TestDismissInboxItem_AgentErrorKindRouted(t *testing.T) {
	deps := newTestDeps(t)
	stub := &stubMarkFixedHandler{}
	deps.svc.SetMarkFixedHandler(stub)

	w := postDismiss(t, deps, `{"kind":"agent_error","item_id":"ae-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if len(stub.agentErrorCalls) != 1 || stub.agentErrorCalls[0] != "ae-1" {
		t.Fatalf("agent error calls = %v, want [ae-1]", stub.agentErrorCalls)
	}
}

func TestDismissInboxItem_AgentErrorNotFoundIs404(t *testing.T) {
	deps := newTestDeps(t)
	deps.svc.SetMarkFixedHandler(&stubMarkFixedHandler{
		agentErrorErr: fmt.Errorf("%w: ae-missing", officeservice.ErrInboxItemNotFound),
	})

	w := postDismiss(t, deps, `{"kind":"agent_error","item_id":"ae-missing"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestDismissInboxItem_UnknownKindStillRejected(t *testing.T) {
	deps := newTestDeps(t)
	deps.svc.SetMarkFixedHandler(&stubMarkFixedHandler{})

	w := postDismiss(t, deps, `{"kind":"nope","item_id":"x"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func readInbox(t *testing.T, deps *testDeps) dashboard.InboxResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/office/workspaces/ws1/inbox", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("inbox status = %d: %s", w.Code, w.Body.String())
	}
	var resp dashboard.InboxResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode inbox: %v", err)
	}
	return resp
}

func readInboxCount(t *testing.T, deps *testDeps) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/office/workspaces/ws1/inbox?count=true", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("count status = %d: %s", w.Code, w.Body.String())
	}
	var resp dashboard.InboxCountResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	return resp.Count
}

func hasInboxType(items []*models.InboxItem, typ string) bool {
	for _, item := range items {
		if item.Type == typ {
			return true
		}
	}
	return false
}

func TestInbox_AgentErrorClearedByLaterSuccess(t *testing.T) {
	deps := newTestDeps(t)

	// The failed escalation for (agent-1, t-1).
	mustExec(t, deps, `INSERT INTO runs
		(id, agent_profile_id, reason, payload, status, error_message, requested_at, finished_at)
		VALUES ('run-1','agent-1','test','{"task_id":"t-1"}','failed','boom','2026-01-01 09:00:00','2026-01-01 10:00:00')`)
	seedInboxAgentError(t, deps, "ae-1", "agent-1", "run-1", "2026-01-01 10:00:01")

	resp := readInbox(t, deps)
	if !hasInboxType(resp.Items, "agent_error") {
		t.Fatalf("agent_error item missing before success: %+v", resp.Items)
	}
	before := readInboxCount(t, deps)

	// A later success in the same (agent, task) scope clears the error.
	mustExec(t, deps, `INSERT INTO runs
		(id, agent_profile_id, reason, payload, status, error_message, requested_at, finished_at)
		VALUES ('ok-1','agent-1','test','{"task_id":"t-1"}','finished','','2026-01-02 09:00:00','2026-01-02 10:00:00')`)

	resp = readInbox(t, deps)
	if hasInboxType(resp.Items, "agent_error") {
		t.Fatalf("agent_error survived a later same-scope success: %+v", resp.Items)
	}
	after := readInboxCount(t, deps)
	if after != before-1 {
		t.Fatalf("count = %d, want %d (the cleared error leaves both list and count)", after, before-1)
	}
}

func TestInbox_AgentErrorTasklessSuccessDoesNotClearTaskedFailure(t *testing.T) {
	deps := newTestDeps(t)

	mustExec(t, deps, `INSERT INTO runs
		(id, agent_profile_id, reason, payload, status, error_message, requested_at, finished_at)
		VALUES ('run-1','agent-1','test','{"task_id":"t-1"}','failed','boom','2026-01-01 09:00:00','2026-01-01 10:00:00')`)
	seedInboxAgentError(t, deps, "ae-1", "agent-1", "run-1", "2026-01-01 10:00:01")

	// A taskless success must not clear a tasked failure.
	mustExec(t, deps, `INSERT INTO runs
		(id, agent_profile_id, reason, payload, status, error_message, requested_at, finished_at)
		VALUES ('ok-1','agent-1','test','{}','finished','','2026-01-02 09:00:00','2026-01-02 10:00:00')`)

	resp := readInbox(t, deps)
	if !hasInboxType(resp.Items, "agent_error") {
		t.Fatal("taskless success cleared a tasked failure")
	}
}
