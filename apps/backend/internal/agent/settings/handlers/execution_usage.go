package handlers

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/kandev/kandev/internal/agent/settings/controller"
	"net/http"
	"time"

	"github.com/kandev/kandev/internal/agent/usage"
)

func (h *Handlers) httpReadNativeProfileUsage(c *gin.Context) {
	respondNativeProfileUsage(c, h.controller.ReadNativeProfileUsage, false)
}

func (h *Handlers) httpReadCachedNativeProfileUsage(c *gin.Context) {
	respondNativeProfileUsage(c, h.controller.ReadCachedNativeProfileUsage, true)
}

func respondNativeProfileUsage(c *gin.Context, read func(context.Context, string) ([]usage.ProfileExecutionUsage, error), cachedOnly bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	entries, err := read(ctx, c.Param("id"))
	if errors.Is(err, controller.ErrAgentFeatureDisabled) || errors.Is(err, controller.ErrAgentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "usage unavailable"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage observation unavailable"})
		return
	}
	body := gin.H{"profile_id": c.Param("id"), "executions": entries}
	if cachedOnly {
		body["cached_only"] = true
	}
	c.JSON(http.StatusOK, body)
}
