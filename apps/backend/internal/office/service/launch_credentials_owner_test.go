package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/models"
	"github.com/kandev/kandev/internal/office/service"
)

type launchOwnerClosed interface{ LaunchOwnerClosed() bool }

func newRemintFixture(t *testing.T) (*service.Service, *service.SchedulerIntegration, map[string]string) {
	t.Helper()
	svc := newTestService(t, service.ServiceOptions{TaskStarter: &mockTaskStarter{}})
	svc.SetAgentTokenMinter(fakeAgentTokenMinter{token: "fresh"})
	ctx := context.Background()
	agent := makeAgent("remint-owner", models.AgentRoleWorker)
	if err := svc.CreateAgentInstance(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := svc.QueueRun(ctx, agent.ID, service.RunReasonTaskAssigned, `{"task_id":"t-remint"}`, ""); err != nil {
		t.Fatalf("queue run: %v", err)
	}
	runs, err := svc.ListRuns(ctx, "ws-1")
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: %v (%d)", err, len(runs))
	}
	env := map[string]string{
		"KANDEV_RUN_ID": runs[0].ID, "KANDEV_AGENT_ID": agent.ID, "KANDEV_WORKSPACE_ID": "ws-1",
	}
	return svc, service.NewSchedulerIntegration(svc, time.Second), env
}

func requireLaunchOwnerClosed(t *testing.T, err error) {
	t.Helper()
	var signal launchOwnerClosed
	if !errors.As(err, &signal) || !signal.LaunchOwnerClosed() {
		t.Fatalf("re-mint error = %v, want a launch-owner-closed signal", err)
	}
}

// A queued run still waits for its launch, so its credentials are re-minted.
func TestRemintLaunchCredentials_WaitingRunIsReminted(t *testing.T) {
	_, si, env := newRemintFixture(t)
	got, err := si.RemintLaunchCredentials(context.Background(), "t-remint", "", env)
	if err != nil {
		t.Fatalf("re-mint for a waiting run failed: %v", err)
	}
	if got["KANDEV_RUN_TOKEN"] != "fresh" {
		t.Fatalf("run token = %q, want the freshly minted one", got["KANDEV_RUN_TOKEN"])
	}
}

// A stored ceiling replay for a run that is over, or already launched into a
// session, must not start a second agent.
func TestRemintLaunchCredentials_RunNoLongerWaitingSignalsClosedOwner(t *testing.T) {
	for name, update := range map[string]string{
		"cancelled":     `UPDATE runs SET status = 'cancelled' WHERE id = ?`,
		"finished":      `UPDATE runs SET status = 'finished' WHERE id = ?`,
		"bound session": `UPDATE runs SET status = 'claimed', session_id = 'sess-already' WHERE id = ?`,
		"deleted":       `DELETE FROM runs WHERE id = ?`,
	} {
		t.Run(name, func(t *testing.T) {
			svc, si, env := newRemintFixture(t)
			svc.ExecSQL(t, update, env["KANDEV_RUN_ID"])
			_, err := si.RemintLaunchCredentials(context.Background(), "t-remint", "", env)
			requireLaunchOwnerClosed(t, err)
		})
	}
}

// The direct launch re-mints with the session it just created; a run bound to
// that same session still owns the launch.
func TestRemintLaunchCredentials_SameBoundSessionIsNotClosed(t *testing.T) {
	svc, si, env := newRemintFixture(t)
	svc.ExecSQL(t, `UPDATE runs SET status = 'claimed', session_id = 'sess-own' WHERE id = ?`, env["KANDEV_RUN_ID"])
	if _, err := si.RemintLaunchCredentials(context.Background(), "t-remint", "sess-own", env); err != nil {
		t.Fatalf("re-mint for the run's own session failed: %v", err)
	}
}
