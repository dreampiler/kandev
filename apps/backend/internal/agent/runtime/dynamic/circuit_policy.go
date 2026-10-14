package dynamic

import (
	"strings"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// cappedCircuitUntil bounds legacy Go quota suspensions by the current policy.
// Other legacy codes do not identify their cause or provider reset reliably.
func cappedCircuitUntil(snapshot CircuitSnapshot) time.Time {
	if snapshot.State != CircuitOpen || snapshot.UpdatedAt.IsZero() || snapshot.Code != routingerr.CodeQuotaLimited {
		return snapshot.Until
	}
	if !strings.Contains(snapshot.Key, "|opencode-go/") && !strings.HasSuffix(snapshot.Key, "|opencode-go|paid") {
		return snapshot.Until
	}
	policy := failureCausePolicies[CauseGoUsageLimit]
	var maximum time.Duration
	for _, duration := range policy.ladder {
		if duration > maximum {
			maximum = duration
		}
	}
	if maximum > 0 {
		capAt := snapshot.UpdatedAt.Add(maximum)
		if snapshot.Until.After(capAt) {
			return capAt
		}
	}
	return snapshot.Until
}
