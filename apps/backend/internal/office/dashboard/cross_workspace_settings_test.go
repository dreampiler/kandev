package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/office/crossworkspace"
	"github.com/kandev/kandev/internal/office/dashboard"
	"github.com/kandev/kandev/internal/office/models"
)

// fakeCrossWorkspaceSettings stores the setting in memory and rejects a
// workspace allow-listing itself, standing in for crossworkspace.Service.
type fakeCrossWorkspaceSettings struct {
	stored  models.CrossWorkspaceAccess
	updates int
}

func (f *fakeCrossWorkspaceSettings) Settings(_ context.Context, ws string) (models.CrossWorkspaceAccess, error) {
	out := f.stored
	out.WorkspaceID = ws
	return out, nil
}

func (f *fakeCrossWorkspaceSettings) UpdateSettings(
	ctx context.Context, ws string, in models.CrossWorkspaceAccess,
) (models.CrossWorkspaceAccess, error) {
	for _, id := range in.AllowedWorkspaceIDs {
		if id == ws {
			return models.CrossWorkspaceAccess{}, crossworkspace.NewValidationError("allowed_workspace_ids must not contain the Office workspace itself")
		}
	}
	f.updates++
	f.stored = in
	return f.Settings(ctx, ws)
}

func patchSettings(t *testing.T, deps *testDeps, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/office/workspaces/ws1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)
	return w
}

func TestWorkspaceSettings_CrossWorkspaceAccessRoundTrip(t *testing.T) {
	deps := newTestDeps(t)
	fake := &fakeCrossWorkspaceSettings{}
	deps.svc.SetCrossWorkspaceSettings(fake)

	w := patchSettings(t, deps, `{"cross_workspace_access":{"enabled":true,"allowed_workspace_ids":["ws-aiws"],"coordinator_task_ids":{"ws-aiws":"ctl"}}}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	if !fake.stored.Enabled || fake.stored.CoordinatorTaskIDs["ws-aiws"] != "ctl" {
		t.Fatalf("stored = %+v", fake.stored)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/office/workspaces/ws1/settings", nil)
	rec := httptest.NewRecorder()
	deps.router.ServeHTTP(rec, req)
	var resp dashboard.WorkspaceSettingsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := resp.Settings.CrossWorkspaceAccess
	if got == nil || !got.Enabled || len(got.AllowedWorkspaceIDs) != 1 || got.WorkspaceID != "ws1" {
		t.Fatalf("cross_workspace_access = %+v", got)
	}
}

func TestWorkspaceSettings_CrossWorkspaceAccessInvalidLeavesOtherFieldsUnapplied(t *testing.T) {
	deps := newTestDeps(t)
	fake := &fakeCrossWorkspaceSettings{}
	deps.svc.SetCrossWorkspaceSettings(fake)
	provider := &stubSettingsProvider{}
	deps.svc.SetSettingsProvider(provider)

	w := patchSettings(t, deps, `{"permission_handling_mode":"auto_approve","cross_workspace_access":{"enabled":true,"allowed_workspace_ids":["ws1"]}}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if provider.lastMode != "" || fake.updates != 0 {
		t.Fatalf("mode=%q updates=%d: a refused request must not apply other fields", provider.lastMode, fake.updates)
	}
}

func TestWorkspaceSettings_AgentTokenCannotChangeCrossWorkspaceAccess(t *testing.T) {
	f := newCommentSecurityFixture(t)
	fake := &fakeCrossWorkspaceSettings{}
	f.svc.SetCrossWorkspaceSettings(fake)
	agent := seedCommentAgent(t, f.agentsSvc, f.repo, "agent-a", "ws1")
	token, err := f.agentsSvc.MintRuntimeJWT(agent.ID, "", agent.WorkspaceID, "run-1", "sess-1", "")
	if err != nil {
		t.Fatalf("mint jwt: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/office/workspaces/ws1/settings",
		strings.NewReader(`{"cross_workspace_access":{"enabled":true}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || fake.updates != 0 {
		t.Fatalf("status = %d updates = %d; an agent must not widen its own reach", rec.Code, fake.updates)
	}
}
