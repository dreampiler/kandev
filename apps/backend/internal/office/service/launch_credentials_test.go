package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/service"
)

// recordingTokenMinter captures the runtime-token arguments the Office
// credential re-minter passes, so a test can assert the token is scoped to
// the launch session (with the run's persisted session as the fallback).
type recordingTokenMinter struct {
	token     string
	calls     int
	sessionID string
}

func (m *recordingTokenMinter) MintRuntimeJWT(_, _, _, _, sessionID, _ string) (string, error) {
	m.calls++
	m.sessionID = sessionID
	return m.token, nil
}

// TestRemintLaunchCredentials_ScopesTokenToLaunchSession pins the P45a
// contract: the re-mint carries the session the launch actually uses. A
// launch with no session yet (a ceiling-deferred replay) falls back to the
// run's persisted session id, and an env with no Office run identity
// round-trips unchanged without minting.
func TestRemintLaunchCredentials_ScopesTokenToLaunchSession(t *testing.T) {
	svc := newTestService(t, service.ServiceOptions{APIBaseURL: "http://localhost:8080/api/v1"})
	minter := &recordingTokenMinter{token: "run-token"}
	svc.SetAgentTokenMinter(minter)
	si := service.NewSchedulerIntegration(svc, time.Minute)
	ctx := context.Background()

	svc.ExecSQL(t, `INSERT INTO runs (id, agent_profile_id, reason, payload, status, session_id, requested_at)
		VALUES ('run-remint-1', 'agent-1', 'task_assigned', '{}', 'queued', 'session-persisted', datetime('now'))`)

	officeEnv := map[string]string{
		"KANDEV_RUN_ID":       "run-remint-1",
		"KANDEV_AGENT_ID":     "agent-1",
		"KANDEV_WORKSPACE_ID": "ws-1",
	}

	got, err := si.RemintLaunchCredentials(ctx, "task-1", "session-launch", officeEnv)
	if err != nil {
		t.Fatalf("remint with launch session: %v", err)
	}
	if minter.sessionID != "session-launch" {
		t.Fatalf("token session id = %q, want %q", minter.sessionID, "session-launch")
	}
	if got["KANDEV_API_KEY"] != "run-token" || got["KANDEV_RUN_TOKEN"] != "run-token" {
		t.Fatalf("refreshed env = %v, want run-token bearer keys", got)
	}
	if officeEnv["KANDEV_API_KEY"] != "" {
		t.Fatal("original env mutated; re-mint must return a copy")
	}

	if _, err := si.RemintLaunchCredentials(ctx, "task-1", "", officeEnv); err != nil {
		t.Fatalf("remint with empty session: %v", err)
	}
	if minter.sessionID != "session-persisted" {
		t.Fatalf("fallback session id = %q, want %q", minter.sessionID, "session-persisted")
	}

	before := minter.calls
	plainEnv := map[string]string{"KANDEV_SOMETHING_ELSE": "x"}
	unchanged, err := si.RemintLaunchCredentials(ctx, "task-1", "session-launch", plainEnv)
	if err != nil {
		t.Fatalf("remint non-office env: %v", err)
	}
	if minter.calls != before {
		t.Fatal("non-Office env triggered a token mint")
	}
	if unchanged["KANDEV_SOMETHING_ELSE"] != "x" {
		t.Fatalf("non-Office env changed: %v", unchanged)
	}
}
