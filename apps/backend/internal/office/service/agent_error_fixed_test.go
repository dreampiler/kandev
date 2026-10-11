package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/service"
)

func insertAgentErrorActivity(
	t *testing.T, svc *service.Service, id, workspaceID, agentID string,
) {
	t.Helper()
	svc.ExecSQL(t, `INSERT INTO office_activity_log
		(id, workspace_id, actor_type, actor_id, action, target_type, target_id, details, run_id, session_id, created_at)
		VALUES (?, ?, 'system', 'scheduler', 'agent.error', 'agent', ?, '{}', ?, '', ?)`,
		id, workspaceID, agentID, id+"-run", time.Now().UTC())
}

func TestMarkAgentErrorFixed_DismissesStoredEntry(t *testing.T) {
	svc, _ := newTestServiceWithBus(t)
	ctx := context.Background()
	createTestAgent(t, svc, "ws-1", "agent-err")
	insertAgentErrorActivity(t, svc, "err-entry-1", "ws-1", "agent-err")

	if err := svc.MarkAgentErrorFixed(ctx, "user-1", "err-entry-1"); err != nil {
		t.Fatalf("mark agent error fixed: %v", err)
	}
	dismissed, err := svc.IsInboxItemDismissed(ctx, "user-1", service.InboxKindAgentError, "err-entry-1")
	if err != nil {
		t.Fatalf("check dismissal: %v", err)
	}
	if !dismissed {
		t.Fatal("agent error entry was not dismissed")
	}
}

func TestMarkAgentErrorFixed_UnmatchedIdentityDismissesNothing(t *testing.T) {
	svc, _ := newTestServiceWithBus(t)
	ctx := context.Background()

	err := svc.MarkAgentErrorFixed(ctx, "user-1", "missing-entry")
	if !errors.Is(err, service.ErrInboxItemNotFound) {
		t.Fatalf("err = %v, want ErrInboxItemNotFound", err)
	}
	dismissed, derr := svc.IsInboxItemDismissed(ctx, "user-1", service.InboxKindAgentError, "missing-entry")
	if derr != nil {
		t.Fatalf("check dismissal: %v", derr)
	}
	if dismissed {
		t.Fatal("unmatched identity wrote a dismissal")
	}
}

func TestMarkAgentErrorFixed_NonAgentErrorEntryDismissesNothing(t *testing.T) {
	svc, _ := newTestServiceWithBus(t)
	ctx := context.Background()
	svc.ExecSQL(t, `INSERT INTO office_activity_log
		(id, workspace_id, actor_type, actor_id, action, target_type, target_id, details, run_id, session_id, created_at)
		VALUES ('act-1', 'ws-1', 'system', 'x', 'agent.paused', 'agent', 'agent-err', '{}', '', '', ?)`,
		time.Now().UTC())

	err := svc.MarkAgentErrorFixed(ctx, "user-1", "act-1")
	if !errors.Is(err, service.ErrInboxItemNotFound) {
		t.Fatalf("err = %v, want ErrInboxItemNotFound for a non agent.error entry", err)
	}
}
