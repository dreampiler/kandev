package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/auth/authn"
	"github.com/kandev/kandev/internal/common/httpmw"
)

func TestReleaseResourceCircuitEndpointIsAvailable(t *testing.T) {
	router, _, _ := newSettingsHarness(t, newFakeSettingsRepo(), nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent-circuits/release", strings.NewReader(`{"resource_key":"model:one"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(httpmw.InterimSettingsInterlockHeader, "test-interlock")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired release = %d, want 503", response.Code)
	}
}

type circuitManager struct{ registry *dynamic.CircuitRegistry }

func (m circuitManager) ReleaseAccountBlock(_ context.Context, id string) (bool, error) {
	return m.registry.ReleaseHold("account_block:" + id)
}
func (m circuitManager) ReleaseResourceCircuit(_ context.Context, key string) (bool, error) {
	return m.registry.Release(key)
}
func (m circuitManager) ListResourceCircuits(_ context.Context) ([]dynamic.CircuitSnapshot, error) {
	return m.registry.ListSuspended(), nil
}

func TestReleaseResourceCircuitEndpointListsAndReleasesOnlySelectedKey(t *testing.T) {
	router, ctrl, _ := newSettingsHarness(t, newFakeSettingsRepo(), nil)
	registry := dynamic.NewCircuitRegistry()
	registry.Open("model:one", time.Now().Add(time.Hour), routingerr.CodeQuotaLimited)
	registry.Hold("account_block:sibling", routingerr.CodeAccountBlocked)
	ctrl.SetAccountBlockReleaser(circuitManager{registry})
	for _, tc := range []struct {
		key   string
		code  int
		token string
	}{
		{"model:one", 200, `"released":true`},
		{"model:one", 200, `"released":false`},
		{"bad:one", 400, "invalid resource circuit key"},
	} {
		payload, _ := json.Marshal(map[string]string{"resource_key": tc.key})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent-circuits/release", strings.NewReader(string(payload)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(httpmw.InterimSettingsInterlockHeader, "test-interlock")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.code || !strings.Contains(response.Body.String(), tc.token) {
			t.Fatalf("release %s = %d %s", tc.key, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/agent-circuits", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "account_block:sibling") || strings.Contains(response.Body.String(), "model:one") {
		t.Fatalf("remaining circuits = %d %s", response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent-circuits/release", strings.NewReader(`{"resource_key":"account_block:sibling"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code == 200 || registry.Inspect("account_block:sibling", time.Now()).State != dynamic.ResourceWaiting {
		t.Fatal("release bypassed the settings mutation interlock")
	}
}

func TestResourceCircuitEndpointsRequireSettingsOwner(t *testing.T) {
	router, ctrl, _ := newSettingsHarnessAs(t, newFakeSettingsRepo(), nil,
		authn.Identity{UserID: "member", Role: authn.RoleMember})
	registry := dynamic.NewCircuitRegistry()
	registry.Open("model:one", time.Now().Add(time.Hour), routingerr.CodeRateLimited)
	ctrl.SetAccountBlockReleaser(circuitManager{registry})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/v1/agent-circuits"
		if method == http.MethodPost {
			path += "/release"
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{"resource_key":"model:one"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(httpmw.InterimSettingsInterlockHeader, "test-interlock")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("member %s = %d, want 403", method, response.Code)
		}
	}
	if registry.Inspect("model:one", time.Now()).State != dynamic.ResourceWaiting {
		t.Fatal("member released a circuit")
	}
}
