package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/agent/settings/controller"
)

// httpPreviewDynamicProfile answers which candidate a dynamic profile would
// choose now. It never saves settings, claims a generation or probe lease,
// opens a circuit or launches an agent, so it is registered without the
// mutation interlock and remains available while settings are locked.
//
// The body is the canonical draft candidate document, so a create-time preview
// works before the profile is saved. The path ID is accepted for a stable client
// request identity; the draft itself carries the candidates under evaluation.
func (h *Handlers) httpPreviewDynamicProfile(c *gin.Context) {
	var req controller.DynamicPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dynamic preview request"})
		return
	}
	resp, err := h.controller.PreviewDynamicProfile(c.Request.Context(), req)
	if err != nil {
		if isInvalidDynamicProfileUpdateError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		h.logger.Error("failed to preview dynamic profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to preview dynamic profile"})
		return
	}
	c.JSON(http.StatusOK, resp)
}
