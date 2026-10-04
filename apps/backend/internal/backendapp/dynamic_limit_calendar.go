package backendapp

import (
	"context"
	"strings"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

// profileUsageReader is the observed-usage seam of one concrete profile.
type profileUsageReader interface {
	GetUsage(ctx context.Context, profileID string) (*agentusage.ProviderUsage, error)
}

// providerLimitLister is the operator-entered provider settings seam.
type providerLimitLister interface {
	ListProviderLimits(ctx context.Context) ([]dynamicruntime.ProviderLimit, error)
}

// dynamicLimitCalendar answers the reset instants a usage-limit suspension
// may wait for. Observed provider usage wins over operator settings, because
// it is the provider's own account state.
type dynamicLimitCalendar struct {
	usage   profileUsageReader
	limits  providerLimitLister
	timeout time.Duration
}

const limitCalendarUsageTimeout = 5 * time.Second

// minMonthlyWindow tells a monthly usage window from a five-hour or weekly one
// when the provider label is not "monthly".
const minMonthlyWindow = 27 * 24 * time.Hour

func newDynamicLimitCalendar(usage profileUsageReader, limits providerLimitLister) *dynamicLimitCalendar {
	return &dynamicLimitCalendar{usage: usage, limits: limits, timeout: limitCalendarUsageTimeout}
}

// ExhaustedUntil implements dynamic.LimitCalendar.
func (c *dynamicLimitCalendar) ExhaustedUntil(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	now time.Time,
) (time.Time, bool) {
	var until time.Time
	for _, window := range c.observedWindows(ctx, candidate) {
		if !window.Exhausted() || !window.ResetAt.After(now) {
			continue
		}
		if window.ResetAt.After(until) {
			until = window.ResetAt
		}
	}
	return until, !until.IsZero()
}

// MonthlyReset implements dynamic.LimitCalendar.
func (c *dynamicLimitCalendar) MonthlyReset(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
	now time.Time,
) (time.Time, bool) {
	if reset, ok := observedMonthlyReset(c.observedWindows(ctx, candidate), now); ok {
		return reset, true
	}
	limit, ok := c.providerLimit(ctx, dynamicruntime.ProviderOf(candidate.ModelID))
	if !ok {
		return time.Time{}, false
	}
	return limit.NextMonthlyReset(now)
}

// observedWindows returns the usage windows that are this candidate's own
// account state. A remote execution authenticates elsewhere, so the host's
// reading is never borrowed for it.
func (c *dynamicLimitCalendar) observedWindows(
	ctx context.Context,
	candidate dynamicruntime.Candidate,
) []agentusage.UtilizationWindow {
	if c == nil || c.usage == nil || candidate.RemoteExecution || candidate.ID == "" {
		return nil
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	observed, err := c.usage.GetUsage(ctx, candidate.ID)
	if err != nil || observed == nil {
		return nil
	}
	windows := make([]agentusage.UtilizationWindow, 0, len(observed.Windows))
	for _, window := range observed.Windows {
		if window.ModelID != "" && window.ModelID != candidate.ModelID {
			continue
		}
		windows = append(windows, window)
	}
	return windows
}

func (c *dynamicLimitCalendar) providerLimit(ctx context.Context, provider string) (dynamicruntime.ProviderLimit, bool) {
	if c == nil || c.limits == nil || provider == "" {
		return dynamicruntime.ProviderLimit{}, false
	}
	limits, err := c.limits.ListProviderLimits(ctx)
	if err != nil {
		return dynamicruntime.ProviderLimit{}, false
	}
	for _, limit := range limits {
		if dynamicruntime.NormalizeProvider(limit.Provider) == provider {
			return limit, true
		}
	}
	return dynamicruntime.ProviderLimit{}, false
}

// observedMonthlyReset returns the earliest future reset among monthly usage
// windows.
func observedMonthlyReset(windows []agentusage.UtilizationWindow, now time.Time) (time.Time, bool) {
	var reset time.Time
	for _, window := range windows {
		if !isMonthlyWindow(window) || !window.ResetAt.After(now) {
			continue
		}
		if reset.IsZero() || window.ResetAt.Before(reset) {
			reset = window.ResetAt
		}
	}
	return reset, !reset.IsZero()
}

func isMonthlyWindow(window agentusage.UtilizationWindow) bool {
	if strings.EqualFold(strings.TrimSpace(window.Label), "monthly") {
		return true
	}
	return time.Duration(window.DurationSeconds)*time.Second >= minMonthlyWindow
}
