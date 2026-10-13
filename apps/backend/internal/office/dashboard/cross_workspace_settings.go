package dashboard

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/office/agents"
	"github.com/kandev/kandev/internal/office/crossworkspace"
	"github.com/kandev/kandev/internal/office/models"
)

// CrossWorkspaceSettings reads and replaces an Office workspace's
// cross-workspace access setting. Satisfied by *crossworkspace.Service.
type CrossWorkspaceSettings interface {
	Settings(ctx context.Context, workspaceID string) (models.CrossWorkspaceAccess, error)
	UpdateSettings(ctx context.Context, workspaceID string, in models.CrossWorkspaceAccess) (models.CrossWorkspaceAccess, error)
}

// CrossWorkspaceAccessRequest is the settings PATCH body for the setting. It
// replaces the whole setting.
type CrossWorkspaceAccessRequest struct {
	Enabled             bool              `json:"enabled"`
	AllowedWorkspaceIDs []string          `json:"allowed_workspace_ids"`
	CoordinatorTaskIDs  map[string]string `json:"coordinator_task_ids"`
}

// SetCrossWorkspaceSettings wires the cross-workspace access setting into
// the workspace settings routes. nil leaves the setting out of the response
// and rejects updates.
func (s *DashboardService) SetCrossWorkspaceSettings(settings CrossWorkspaceSettings) {
	s.crossWorkspace = settings
}

// crossWorkspaceAccess returns the setting for the settings response, or nil
// when the setting is unavailable or unreadable.
func (s *DashboardService) crossWorkspaceAccess(ctx context.Context, workspaceID string) *models.CrossWorkspaceAccess {
	if s.crossWorkspace == nil {
		return nil
	}
	access, err := s.crossWorkspace.Settings(ctx, workspaceID)
	if err != nil {
		return nil
	}
	return &access
}

// applyCrossWorkspaceAccess handles the cross_workspace_access field of a
// settings PATCH. It reports whether the request was answered (with an
// error status); the caller stops on true.
//
// Only a person may change the setting: an Office agent token, which the
// route otherwise accepts for its own workspace, must not widen its own
// reach.
func (h *Handler) applyCrossWorkspaceAccess(c *gin.Context, wsID string, req *CrossWorkspaceAccessRequest) bool {
	if req == nil {
		return false
	}
	if agents.CallerFromContext(c) != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "cross_workspace_access can only be changed by a person"})
		return true
	}
	if h.svc.crossWorkspace == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cross-workspace access is not available"})
		return true
	}
	_, err := h.svc.crossWorkspace.UpdateSettings(c.Request.Context(), wsID, models.CrossWorkspaceAccess{
		Enabled:             req.Enabled,
		AllowedWorkspaceIDs: req.AllowedWorkspaceIDs,
		CoordinatorTaskIDs:  req.CoordinatorTaskIDs,
	})
	if err == nil {
		return false
	}
	var validation *crossworkspace.ValidationError
	if errors.As(err, &validation) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return true
	}
	h.logger.Error("failed to update cross_workspace_access",
		zap.String("workspace", wsID), zap.Error(err))
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update cross_workspace_access"})
	return true
}
