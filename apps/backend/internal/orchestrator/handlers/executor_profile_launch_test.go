package handlers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/orchestrator"
	"github.com/kandev/kandev/internal/task/repository"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	ws "github.com/kandev/kandev/pkg/websocket"
	"github.com/stretchr/testify/require"
)

type profileLaunchAttachmentClaimer struct {
	calls int
}

func (c *profileLaunchAttachmentClaimer) ClaimMessageAttachments(context.Context, string, string, []v1.MessageAttachment) error {
	c.calls++
	return errors.New("attachment boundary reached")
}

func TestWSLaunchExecutorProfileReference(t *testing.T) {
	conn, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	database := sqlx.NewDb(conn, "sqlite3")
	t.Cleanup(func() { _ = database.Close() })
	repo, cleanup, err := repository.Provide(database, database, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "console", OutputPath: "stderr"})
	require.NoError(t, err)
	eventBus := bus.NewMemoryEventBus(log)
	t.Cleanup(eventBus.Close)
	svc := orchestrator.NewService(orchestrator.ServiceConfig{}, eventBus, &archivedLaunchAgentManager{}, nil, repo, nil, nil, nil, log)
	claimer := &profileLaunchAttachmentClaimer{}
	svc.SetLaunchAttachmentClaimer(claimer)
	h := NewHandlers(svc, log)
	resp, err := h.wsLaunchSession(context.Background(), createTestMessage(t, ws.ActionSessionLaunch, map[string]interface{}{
		"task_id": "task", "intent": "start", "executor_profile_id": "missing-profile",
		"attachments": []v1.MessageAttachment{{AttachmentID: "attachment"}},
	}))
	require.NoError(t, err)
	payload := parseError(t, resp)
	require.Equal(t, ws.ErrorCodeNotFound, payload.Code)
	require.Contains(t, payload.Message, "missing-profile")
	require.Equal(t, "executor_profile_not_found", payload.Details["error_code"])
	require.Zero(t, claimer.calls)
	resp, err = h.wsLaunchSession(context.Background(), createTestMessage(t, ws.ActionSessionLaunch, map[string]interface{}{
		"task_id": "task", "intent": "start", "attachments": []v1.MessageAttachment{{AttachmentID: "attachment"}},
	}))
	require.NoError(t, err)
	require.Equal(t, ws.ErrorCodeInternalError, parseError(t, resp).Code)
	require.Equal(t, 1, claimer.calls)
}
