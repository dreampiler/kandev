package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestCrossWorkspaceCLI_ReadPaths(t *testing.T) {
	cases := []struct {
		args      []string
		wantPath  string
		wantQuery url.Values
	}{
		{[]string{"workspaces", "list"}, "/api/v1/office/runtime/workspaces", url.Values{}},
		{[]string{"workspaces", "coordinator", "--id", "ws-b"}, "/api/v1/office/runtime/workspaces/ws-b/coordinator", url.Values{}},
		{[]string{"workspaces", "tasks", "--id", "ws-b", "--workflow", "wf-1"}, "/api/v1/office/runtime/workspaces/ws-b/tasks", url.Values{"workflow_id": {"wf-1"}}},
		{[]string{"tasks", "list", "--workspace", "ws-b"}, "/api/v1/office/runtime/workspaces/ws-b/tasks", url.Values{}},
		{[]string{"tasks", "sessions", "--id", "t-b"}, "/api/v1/office/runtime/tasks/t-b/sessions", url.Values{}},
		{[]string{"tasks", "plan", "--id", "t-b"}, "/api/v1/office/runtime/tasks/t-b/plan", url.Values{}},
		{[]string{"tasks", "transcript", "--id", "t-b", "--limit", "5", "--sort", "desc"},
			"/api/v1/office/runtime/tasks/t-b/conversation", url.Values{"limit": {"5"}, "sort": {"desc"}}},
	}
	for _, tc := range cases {
		t.Run(tc.wantPath, func(t *testing.T) {
			srv, captured := setupMockServer(t, http.StatusOK, `{}`)
			setEnvVars(t, srv)
			if code := runKandevCLI(tc.args); code != 0 {
				t.Fatalf("exit %d", code)
			}
			if captured.Method != http.MethodGet || captured.Path != tc.wantPath {
				t.Fatalf("%s %s, want GET %s", captured.Method, captured.Path, tc.wantPath)
			}
			got, _ := url.ParseQuery(captured.Query)
			for key, want := range tc.wantQuery {
				if got.Get(key) != want[0] {
					t.Fatalf("query %s = %q, want %q", key, got.Get(key), want[0])
				}
			}
			assertAuthHeader(t, captured)
		})
	}
}

func TestCrossWorkspaceCLI_SendPostsPromptAsRunTask(t *testing.T) {
	srv, captured := setupMockServer(t, http.StatusOK, `{"status":"queued"}`)
	setEnvVars(t, srv)
	code := runKandevCLI([]string{"tasks", "send", "--id", "ctl-b", "--prompt", "please pause W-12", "--operation-id", "op-1"})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if captured.Method != http.MethodPost || captured.Path != "/api/v1/office/runtime/tasks/ctl-b/messages" {
		t.Fatalf("%s %s", captured.Method, captured.Path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(captured.Body), &body); err != nil {
		t.Fatalf("body %q: %v", captured.Body, err)
	}
	if body["prompt"] != "please pause W-12" || body["operation_id"] != "op-1" || len(body) != 2 {
		t.Fatalf("body = %v; the sender must come from the run token, not the request", body)
	}
}

func TestCrossWorkspaceCLI_RequiresIDs(t *testing.T) {
	srv, _ := setupMockServer(t, http.StatusOK, `{}`)
	setEnvVars(t, srv)
	for _, args := range [][]string{
		{"workspaces", "coordinator"},
		{"workspaces", "tasks"},
		{"tasks", "send", "--prompt", "x"},
	} {
		if code := runKandevCLI(args); code == 0 {
			t.Fatalf("%v: exit 0, want failure", args)
		}
	}
}
