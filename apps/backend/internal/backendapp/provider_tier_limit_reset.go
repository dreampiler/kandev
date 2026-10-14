package backendapp

import (
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"regexp"
	"strconv"
	"time"
)

var rateResetHeader = regexp.MustCompile(`(?i)x-ratelimit-reset["'\\]*\s*[:=]\s*["'\\]*([0-9T:.+Z-]+)`)

func minuteResetFromFailure(failure *routingerr.Error, now time.Time) time.Time {
	if match := rateResetHeader.FindStringSubmatch(failure.RawExcerpt); len(match) == 2 {
		if reset, ok := parseRateReset(match[1]); ok && reset.After(now) {
			return reset
		}
	}
	if failure.ResetHint != nil && failure.ResetHint.After(now) {
		return *failure.ResetHint
	}
	return now.Add(time.Minute)
}

func parseRateReset(value string) (time.Time, bool) {
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n > 100_000_000_000 {
			return time.UnixMilli(n), true
		}
		return time.Unix(n, 0), true
	}
	reset, err := time.Parse(time.RFC3339, value)
	return reset, err == nil
}
