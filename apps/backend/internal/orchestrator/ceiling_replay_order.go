package orchestrator

import (
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// ceilingReplayLess orders the global replay queue independently of per-step
// position-first admission. Unrelated writes never participate in this order.
func ceilingReplayLess(left, right *models.Task) bool {
	if lp, rp := models.TaskPriorityRank(left.Priority), models.TaskPriorityRank(right.Priority); lp != rp {
		return lp < rp
	}
	if left.Position != right.Position {
		return left.Position < right.Position
	}
	leftTime, rightTime := ceilingReplayQueuedAt(left), ceilingReplayQueuedAt(right)
	if leftTime.IsZero() != rightTime.IsZero() {
		return !leftTime.IsZero()
	}
	if !leftTime.Equal(rightTime) {
		return leftTime.Before(rightTime)
	}
	return left.ID < right.ID
}

func ceilingReplayQueuedAt(task *models.Task) time.Time {
	record, _ := task.Metadata[models.MetaKeyDeferredLaunch].(map[string]interface{})
	deferral, err := models.ReadCeilingDeferral(record)
	if err != nil {
		return time.Time{}
	}
	return deferral.QueuedAt
}
