package dashboard

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
	"go.uber.org/zap"
)

type controlReadRepository interface {
	ListControlTasks(context.Context, string, string, string, int, time.Time) (*sqlite.ControlTaskList, error)
	ReadControlMetrics(context.Context, string, time.Time) (*sqlite.ControlMetrics, error)
	ReadControlHistory(context.Context, string, time.Time) (*sqlite.ControlHistory, error)
}

// Existing :wsId routes retain their workspace middleware and agent policy.
// Opt-in projection avoids changing legacy dashboard/list payloads.
func (h *Handler) controlRead(c *gin.Context, section string) bool {
	if c.Query("view") != "control" {
		return false
	}
	reader, ok := h.labels.(controlReadRepository)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "control projection unavailable"})
		return true
	}
	if hours := c.Query("window_hours"); hours != "" && hours != "24" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "window_hours must be 24"})
		return true
	}
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var data any
	var err error
	switch section {
	case "activity":
		data, err = reader.ReadControlHistory(ctx, c.Param("wsId"), now)
	case "dashboard":
		data, err = reader.ReadControlMetrics(ctx, c.Param("wsId"), now)
	case "tasks":
		data, err = h.controlTasks(c, ctx, reader, now)
	}
	if c.IsAborted() {
		return true
	}
	if err != nil {
		h.logger.Warn("control projection read failed", zap.String("section", section), zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "control projection unavailable"})
		return true
	}
	c.JSON(http.StatusOK, data)
	return true
}

func (h *Handler) controlTasks(c *gin.Context, ctx context.Context, reader controlReadRepository, now time.Time) (*sqlite.ControlTaskList, error) {
	filter := c.DefaultQuery("filter", "all")
	archive := c.DefaultQuery("archive", "active")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || (limit != -1 && (limit < 1 || limit > 500)) || !controlFilterValid(filter) || !controlArchiveValid(archive) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid control task filter, archive or limit"})
		return nil, nil
	}
	return reader.ListControlTasks(ctx, c.Param("wsId"), filter, archive, limit, now)
}

func controlFilterValid(filter string) bool {
	switch filter {
	case "all", "active", "sessions", "queued", "hold", "problems", "completed":
		return true
	}
	return false
}

func controlArchiveValid(archive string) bool {
	return archive == "active" || archive == "archived" || archive == "all"
}
