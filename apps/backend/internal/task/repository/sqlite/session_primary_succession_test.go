package sqlite

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

func seedSuccessionSession(t *testing.T, repo *Repository, taskID, sessionID string, state models.TaskSessionState) {
	t.Helper()
	if err := repo.CreateTaskSession(context.Background(), &models.TaskSession{
		ID:     sessionID,
		TaskID: taskID,
		State:  state,
	}); err != nil {
		t.Fatalf("create session %s: %v", sessionID, err)
	}
}

func primarySessionIDs(t *testing.T, repo *Repository, taskID string) []string {
	t.Helper()
	sessions, err := repo.ListTaskSessions(context.Background(), taskID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	var ids []string
	for _, session := range sessions {
		if session != nil && session.IsPrimary {
			ids = append(ids, session.ID)
		}
	}
	return ids
}

func newSuccessionTask(t *testing.T, repo *Repository, taskID string) {
	t.Helper()
	if err := repo.CreateTask(context.Background(), &models.Task{ID: taskID, Title: "Succession"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
}

func TestSetSessionPrimaryIfCurrentPrimaryTerminalPromotesOverTerminal(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	newSuccessionTask(t, repo, "t-succ")
	seedSuccessionSession(t, repo, "t-succ", "s-terminal", models.TaskSessionStateFailed)
	seedSuccessionSession(t, repo, "t-succ", "s-new", models.TaskSessionStateRunning)
	if err := repo.SetSessionPrimary(ctx, "s-terminal"); err != nil {
		t.Fatalf("seed primary: %v", err)
	}

	promoted, err := repo.SetSessionPrimaryIfCurrentPrimaryTerminal(ctx, "s-new")
	if err != nil || !promoted {
		t.Fatalf("promote over terminal primary = (%t, %v), want (true, nil)", promoted, err)
	}
	if ids := primarySessionIDs(t, repo, "t-succ"); len(ids) != 1 || ids[0] != "s-new" {
		t.Fatalf("primaries = %v, want [s-new]", ids)
	}
}

func TestSetSessionPrimaryIfCurrentPrimaryTerminalKeepsLivePrimary(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	newSuccessionTask(t, repo, "t-live")
	seedSuccessionSession(t, repo, "t-live", "s-live", models.TaskSessionStateRunning)
	seedSuccessionSession(t, repo, "t-live", "s-new", models.TaskSessionStateRunning)
	if err := repo.SetSessionPrimary(ctx, "s-live"); err != nil {
		t.Fatalf("seed primary: %v", err)
	}

	promoted, err := repo.SetSessionPrimaryIfCurrentPrimaryTerminal(ctx, "s-new")
	if err != nil || promoted {
		t.Fatalf("promote over live primary = (%t, %v), want (false, nil)", promoted, err)
	}
	if ids := primarySessionIDs(t, repo, "t-live"); len(ids) != 1 || ids[0] != "s-live" {
		t.Fatalf("primaries = %v, want [s-live]", ids)
	}
}

func TestSetSessionPrimaryIfCurrentPrimaryTerminalDeclinesTerminalTarget(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	newSuccessionTask(t, repo, "t-dead")
	seedSuccessionSession(t, repo, "t-dead", "s-terminal", models.TaskSessionStateFailed)
	seedSuccessionSession(t, repo, "t-dead", "s-dead", models.TaskSessionStateCompleted)
	if err := repo.SetSessionPrimary(ctx, "s-terminal"); err != nil {
		t.Fatalf("seed primary: %v", err)
	}

	promoted, err := repo.SetSessionPrimaryIfCurrentPrimaryTerminal(ctx, "s-dead")
	if err != nil || promoted {
		t.Fatalf("promote terminal target = (%t, %v), want (false, nil)", promoted, err)
	}
	if ids := primarySessionIDs(t, repo, "t-dead"); len(ids) != 1 || ids[0] != "s-terminal" {
		t.Fatalf("primaries = %v, want [s-terminal]", ids)
	}
}

func TestSetSessionPrimaryIfCurrentPrimaryTerminalSingleWinner(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	newSuccessionTask(t, repo, "t-race")
	seedSuccessionSession(t, repo, "t-race", "s-terminal", models.TaskSessionStateCancelled)
	seedSuccessionSession(t, repo, "t-race", "s-first", models.TaskSessionStateWaitingForInput)
	seedSuccessionSession(t, repo, "t-race", "s-second", models.TaskSessionStateWaitingForInput)
	if err := repo.SetSessionPrimary(ctx, "s-terminal"); err != nil {
		t.Fatalf("seed primary: %v", err)
	}

	first, err := repo.SetSessionPrimaryIfCurrentPrimaryTerminal(ctx, "s-first")
	if err != nil || !first {
		t.Fatalf("first succession = (%t, %v), want (true, nil)", first, err)
	}
	second, err := repo.SetSessionPrimaryIfCurrentPrimaryTerminal(ctx, "s-second")
	if err != nil || second {
		t.Fatalf("second succession = (%t, %v), want (false, nil)", second, err)
	}
	if ids := primarySessionIDs(t, repo, "t-race"); len(ids) != 1 || ids[0] != "s-first" {
		t.Fatalf("primaries = %v, want [s-first]", ids)
	}
}
