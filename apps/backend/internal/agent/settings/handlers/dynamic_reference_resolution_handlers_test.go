package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kandev/kandev/internal/agent/settings/controller"
)

// TestWriteDynamicResolutionErrorMapsWouldEmptyToConflict pins the HTTP mapping
// for the zero-candidate rejection: it follows the existing conflict contract
// (409) and echoes the named error, so the client's resolution error path can
// surface which dynamic profile blocks the change.
func TestWriteDynamicResolutionErrorMapsWouldEmptyToConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	h := &Handlers{}
	err := fmt.Errorf("%w: %q", controller.ErrDynamicProfileWouldEmpty, "Balanced")
	h.writeDynamicResolutionError(ctx, err)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var body map[string]any
	decodeSettingsJSON(t, recorder, &body)
	message, _ := body["error"].(string)
	if !strings.Contains(message, "Balanced") {
		t.Fatalf("error body = %q, want the blocking profile name", message)
	}
}
