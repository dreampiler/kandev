package dashboard

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kandev/kandev/internal/office/repository/sqlite"
	"github.com/kandev/kandev/internal/system/sessioncapacity"
	"go.uber.org/zap"
)

// ControlCapacitySource is the admission owner's read-only observation. The
// optional seam keeps the control dashboard free of launch or admission work.
type ControlCapacitySource interface {
	SessionCapacityObservation(context.Context) sessioncapacity.Observation
}

// SetControlCapacitySource wires the installation-wide shared-capacity
// observation used by the control dashboard aggregate.
func (s *DashboardService) SetControlCapacitySource(source ControlCapacitySource) {
	if s != nil {
		s.controlCapacity = source
	}
}

// controlCapacityWaiting projects the waiter observation into the aggregate's
// two figures. Grade counts a grade-limit refusal, including both-limit
// refusals; Total counts every refusal. A nil source omits the field.
func (s *DashboardService) controlCapacityWaiting(ctx context.Context) *sqlite.ControlWaiting {
	if s == nil || s.controlCapacity == nil {
		return nil
	}
	waiting := s.controlCapacity.SessionCapacityObservation(ctx).Waiting
	if !waiting.Known {
		return &sqlite.ControlWaiting{}
	}
	total, grade := 0, 0
	for reason, count := range waiting.Counts {
		total += count
		if reason == sessioncapacity.WaitingReasonGradeLimit || reason == sessioncapacity.WaitingReasonBothLimits {
			grade += count
		}
	}
	return &sqlite.ControlWaiting{Known: true, Grade: grade, Total: total}
}

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
		var metrics *sqlite.ControlMetrics
		metrics, err = reader.ReadControlMetrics(ctx, c.Param("wsId"), now)
		if err == nil {
			metrics.SharedCapacityWaiting = h.svc.controlCapacityWaiting(ctx)
		}
		data = metrics
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
