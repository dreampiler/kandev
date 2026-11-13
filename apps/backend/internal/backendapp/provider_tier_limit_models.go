package backendapp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

type modelWindowUsageReader interface {
	GetManualModelWindowUsageForProfiles(context.Context, []string, time.Time, time.Time) ([]sqliterepo.ManualModelWindowUsage, error)
}

var errModelAttributionUnavailable = errors.New("model-attributed usage is unavailable")

// provenAccountLimit uses only account-wide evidence already read before an
// attribution failure. It never treats an unknown model scope as exhausted.
func provenAccountLimit(limits agentusage.TierLimits, used agentusage.LimitUsage, now time.Time) agentusage.LimitAdmission {
	limits.Scopes = nil
	if limits.RequestsPerModel {
		limits.RequestsPerMinute = nil
		limits.RequestsPerWindow = nil
	}
	windows := make([]agentusage.LimitBudget, 0, len(limits.Windows))
	for _, budget := range limits.Windows {
		if budget.Scope == "" {
			windows = append(windows, budget)
		}
	}
	limits.Windows = windows
	return agentusage.EvaluateTierLimits(limits, used, now)
}

// readModelRequestCount never assigns a missing model to the profile's
// current default. Recorded and live observations remain separate lower bounds.
func (s *providerTierLimitService) readModelRequestCount(ctx context.Context, ids, models []string, start, end time.Time) (float64, error) {
	reader, ok := s.store.(modelWindowUsageReader)
	if !ok {
		return 0, errModelAttributionUnavailable
	}
	if len(ids) == 0 {
		return 0, nil
	}
	groups, err := reader.GetManualModelWindowUsageForProfiles(ctx, ids, start, end)
	if err != nil {
		return 0, err
	}
	var recorded float64
	for _, group := range groups {
		if strings.TrimSpace(group.Model) == "" && group.Usage.EventCount > 0 {
			return 0, fmt.Errorf("%w: recorded request model is unknown", errModelAttributionUnavailable)
		}
		for _, wanted := range models {
			if strings.EqualFold(agentusage.ScopeModelID(group.Model), agentusage.ScopeModelID(wanted)) {
				recorded += float64(group.Usage.EventCount)
				break
			}
		}
	}
	live, known := s.requests.countModels(ids, models, start, end)
	if !known {
		return 0, fmt.Errorf("%w: live request model is unknown", errModelAttributionUnavailable)
	}
	return math.Max(recorded, live), nil
}
