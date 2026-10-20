package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kandev/kandev/internal/task/service"
)

// A non-cascade delete whose child changed concurrently is a retryable
// conflict, not a server fault. The delete path joins the error with its
// compensation results, so the mapping must see through errors.Join.
func TestHandleNotFound_TaskDeleteChildConflictIsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	conflict := &service.TaskDeleteChildWorkspaceConflictError{
		ChildID: "child-1", Reason: service.TaskDeleteChildReparented,
	}

	handleNotFound(ctx, newTestLogger(t), errors.Join(conflict), "task not deleted")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var payload struct {
		Error       string `json:"error"`
		ErrorCode   string `json:"error_code"`
		ChildTaskID string `json:"child_task_id"`
		Reason      string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ErrorCode != service.TaskDeleteChildWorkspaceConflictErrorCode {
		t.Fatalf("error_code=%q, want %q", payload.ErrorCode, service.TaskDeleteChildWorkspaceConflictErrorCode)
	}
	if payload.ChildTaskID != "child-1" || payload.Reason != string(service.TaskDeleteChildReparented) {
		t.Fatalf("child_task_id=%q reason=%q, want child-1/%s", payload.ChildTaskID, payload.Reason, service.TaskDeleteChildReparented)
	}
	if payload.Error == "" {
		t.Fatal("error message is empty")
	}
}
