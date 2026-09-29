package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
	"github.com/kandev/kandev/internal/office/models"
)

func createSettingsAgent(t *testing.T, store settingsstore.Repository, id, name string) {
	t.Helper()
	if err := store.CreateAgent(context.Background(), &settingsmodels.Agent{ID: id, Name: name}); err != nil {
		t.Fatalf("create settings agent %s: %v", id, err)
	}
}

func createSettingsProfile(
	t *testing.T, store settingsstore.Repository, id, agentID, workspaceID string,
) {
	t.Helper()
	profile := &settingsmodels.AgentProfile{
		ID:          id,
		AgentID:     agentID,
		Name:        id,
		WorkspaceID: workspaceID,
	}
	if err := store.CreateAgentProfile(context.Background(), profile); err != nil {
		t.Fatalf("create settings profile %s: %v", id, err)
	}
}

type executionProfileResponse struct {
	Agent struct {
		ExecutionAgentProfileID string `json:"execution_agent_profile_id"`
	} `json:"agent"`
	Code string `json:"code"`
}

func decodeExecutionProfileResponse(t *testing.T, body []byte) executionProfileResponse {
	t.Helper()
	var resp executionProfileResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response %q: %v", string(body), err)
	}
	return resp
}

// TestUpdateAgent_ExecutionProfileBinding covers PATCH /agents/:id accepting,
// clearing, and rejecting execution_agent_profile_id bindings.
func TestUpdateAgent_ExecutionProfileBinding(t *testing.T) {
	svc, _, store := newTestAgentServiceWithProfileStore(t)
	ctx := context.Background()
	createSettingsAgent(t, store, "dynamic", "dynamic")
	createSettingsAgent(t, store, "claude-acp", "claude-acp")
	createSettingsProfile(t, store, "dyn-ws1", "dynamic", "ws-1")
	createSettingsProfile(t, store, "concrete", "claude-acp", "ws-1")
	createSettingsProfile(t, store, "dyn-other", "dynamic", "ws-other")

	target := &models.AgentInstance{WorkspaceID: "ws-1", Name: "Worker", Role: models.AgentRoleWorker}
	if err := svc.CreateAgentInstance(ctx, target); err != nil {
		t.Fatalf("create office agent: %v", err)
	}

	t.Run("set binding", func(t *testing.T) {
		rec := newPatchAgentRecorder(t, svc, target.ID, `{"execution_agent_profile_id":"dyn-ws1"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if got := decodeExecutionProfileResponse(t, rec.Body.Bytes()).Agent.ExecutionAgentProfileID; got != "dyn-ws1" {
			t.Fatalf("response binding = %q, want dyn-ws1", got)
		}
		stored, err := svc.GetAgentFromConfig(ctx, target.ID)
		if err != nil {
			t.Fatalf("reload agent: %v", err)
		}
		if stored.ExecutionAgentProfileID != "dyn-ws1" {
			t.Fatalf("stored binding = %q, want dyn-ws1", stored.ExecutionAgentProfileID)
		}
	})

	t.Run("clear binding", func(t *testing.T) {
		rec := newPatchAgentRecorder(t, svc, target.ID, `{"execution_agent_profile_id":""}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		stored, err := svc.GetAgentFromConfig(ctx, target.ID)
		if err != nil {
			t.Fatalf("reload agent: %v", err)
		}
		if stored.ExecutionAgentProfileID != "" {
			t.Fatalf("stored binding = %q, want empty", stored.ExecutionAgentProfileID)
		}
	})

	cases := []struct {
		name     string
		profile  string
		wantCode string
	}{
		{name: "self", profile: target.ID, wantCode: "agent_execution_profile_self"},
		{name: "not dynamic", profile: "concrete", wantCode: "agent_execution_profile_not_dynamic"},
		{name: "missing", profile: "does-not-exist", wantCode: ""},
		{name: "cross workspace", profile: "dyn-other", wantCode: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"execution_agent_profile_id":"` + tc.profile + `"}`
			rec := newPatchAgentRecorder(t, svc, target.ID, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if got := decodeExecutionProfileResponse(t, rec.Body.Bytes()).Code; got != tc.wantCode {
				t.Fatalf("code = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

func TestValidateExecutionProfileBinding(t *testing.T) {
	svc, _, store := newTestAgentServiceWithProfileStore(t)
	ctx := context.Background()
	createSettingsAgent(t, store, "dynamic", "dynamic")
	createSettingsAgent(t, store, "claude-acp", "claude-acp")
	createSettingsProfile(t, store, "dyn-ws1", "dynamic", "ws-1")
	createSettingsProfile(t, store, "concrete", "claude-acp", "ws-1")
	createSettingsProfile(t, store, "dyn-other", "dynamic", "ws-other")

	target := &models.AgentInstance{ID: "agent-1", WorkspaceID: "ws-1"}

	tests := []struct {
		name    string
		profile string
		wantErr error
	}{
		{name: "empty clears", profile: "", wantErr: nil},
		{name: "valid dynamic", profile: "dyn-ws1", wantErr: nil},
		{name: "self rejected", profile: "agent-1", wantErr: ErrAgentExecutionProfileSelf},
		{name: "concrete rejected", profile: "concrete", wantErr: ErrAgentExecutionProfileNotDynamic},
		{name: "cross workspace rejected", profile: "dyn-other", wantErr: nil},
		{name: "missing rejected", profile: "nope", wantErr: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.ValidateExecutionProfileBinding(ctx, target, tc.profile)
			if tc.wantErr == nil {
				if tc.profile == "dyn-other" || tc.profile == "nope" {
					if err == nil {
						t.Fatalf("expected error for %q", tc.profile)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
