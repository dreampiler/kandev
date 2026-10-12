package backendapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	officeagents "github.com/kandev/kandev/internal/office/agents"
)

// fakeOfficeAgentReach lets the token's workspace read exactly the listed
// target workspaces.
type fakeOfficeAgentReach struct {
	office  string
	targets map[string]bool
	calls   int
}

func (f *fakeOfficeAgentReach) Reachable(_ context.Context, office, target string) error {
	f.calls++
	if office == f.office && f.targets[target] {
		return nil
	}
	return errors.New("not reachable")
}

func officeAgentEngineWithReach(
	t *testing.T, h *officeScopeHarness, owner string, reach officeAgentReach, reached *string,
) (*gin.Engine, string) {
	t.Helper()
	agentSvc := officeagents.NewAgentService(h.officeRepo, testLogger(t), nil)
	agentSvc.SetAuth(officeagents.NewAgentAuth("test-signing-key"))
	token, err := agentSvc.MintRuntimeJWT(
		"agent-"+owner, "task-"+owner, h.workspaces[owner], "run-"+owner, "", "")
	if err != nil {
		t.Fatalf("mint runtime jwt: %v", err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1/office")
	group.Use(officeagents.AgentAuthMiddleware(agentSvc))
	group.Use(officeWorkspaceScopeMiddlewareWithReach(h.authSvc, h.taskSvc, h.officeRepo, reach))
	for _, tc := range officeAllScopeCases() {
		pattern := strings.TrimPrefix(tc.pattern, "/api/v1/office")
		group.Handle(tc.method, pattern, func(c *gin.Context) {
			*reached = c.FullPath()
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	}
	return engine, token
}

// TestOfficeScopeAgentReadsReachableWorkspaceOnly pins the one exception the
// Office cross-workspace access setting makes to agent-token confinement: a
// GET of a resource in a workspace the token's Office workspace reaches. A
// write to that same workspace, and any request to a workspace it does not
// reach, stays refused before dispatch.
func TestOfficeScopeAgentReadsReachableWorkspaceOnly(t *testing.T) {
	h := newOfficeScopeHarness(t)
	reach := &fakeOfficeAgentReach{
		office:  h.workspaces[officeScopeUserASecond],
		targets: map[string]bool{h.workspaces[officeScopeUserA]: true},
	}

	for _, tc := range officeScopeCases() {
		if tc.path == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			var reached string
			engine, token := officeAgentEngineWithReach(t, h, officeScopeUserASecond, reach, &reached)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if tc.method == http.MethodGet {
				if rec.Code != http.StatusOK || reached != tc.pattern {
					t.Errorf("reachable read: status = %d (%s), reached = %q", rec.Code, rec.Body.String(), reached)
				}
				return
			}
			if rec.Code != http.StatusNotFound || reached != "" {
				t.Errorf("write across workspaces: status = %d, reached = %q; want 404 before dispatch", rec.Code, reached)
			}
		})
	}
}

func TestOfficeScopeAgentReadOfUnreachableWorkspaceDenied(t *testing.T) {
	h := newOfficeScopeHarness(t)
	reach := &fakeOfficeAgentReach{office: h.workspaces[officeScopeUserASecond], targets: map[string]bool{}}
	var reached string
	engine, token := officeAgentEngineWithReach(t, h, officeScopeUserASecond, reach, &reached)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/office/projects/project-user-a", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || reached != "" {
		t.Fatalf("status = %d, reached = %q; want 404 before dispatch", rec.Code, reached)
	}
	if reach.calls == 0 {
		t.Fatal("the reach decision was never consulted")
	}
}
