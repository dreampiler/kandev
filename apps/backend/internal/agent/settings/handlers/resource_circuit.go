package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kandev/kandev/internal/agent/settings/controller"
)

func (h *Handlers) httpReleaseResourceCircuit(c *gin.Context) {
	var request struct {
		Key string `json:"resource_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource_key is required"})
		return
	}
	released, err := h.controller.ReleaseResourceCircuit(c.Request.Context(), request.Key)
	if errors.Is(err, controller.ErrInvalidResourceCircuitKey) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource circuit key"})
		return
	}
	if h.resourceCircuitError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"released": released})
}

func (h *Handlers) httpListResourceCircuits(c *gin.Context) {
	circuits, err := h.controller.ListResourceCircuits(c.Request.Context())
	if h.resourceCircuitError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"circuits": circuits})
}

func (h *Handlers) resourceCircuitError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, controller.ErrAccountBlockReleaseUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "resource circuit management is unavailable"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "resource circuit operation failed"})
	default:
		return false
	}
	return true
}
