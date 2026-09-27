package routingerr

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// resetHintPattern captures the retry time from provider notices such as
// "try again at Sep 27th, 2026 3:09 AM." The year is optional; the meridiem
// is optional for 24-hour clocks.
var resetHintPattern = regexp.MustCompile(
	`(?i)try again at\s+([A-Za-z]{3,9})[\s,]+(\d{1,2})(?:st|nd|rd|th)?(?:[\s,]+(\d{4}))?[\s,]+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*(AM|PM)?`,
)

var shortMonthNames = [...]string{
	"jan", "feb", "mar", "apr", "may", "jun",
	"jul", "aug", "sep", "oct", "nov", "dec",
}

// parseResetHint extracts a provider-supplied retry time from free text. It
// returns nil when no plausible timestamp is present. The timestamp is a local
// wall-clock time because providers render the notice in the user's locale.
func parseResetHint(text string) *time.Time {
	match := resetHintPattern.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	month, ok := monthFromName(match[1])
	if !ok {
		return nil
	}
	day, ok := parseIntInRange(match[2], 1, 31)
	if !ok {
		return nil
	}
	year, ok := resetYear(match[3])
	if !ok {
		return nil
	}
	hour, ok := parseIntInRange(match[4], 0, 23)
	if !ok {
		return nil
	}
	minute, ok := parseIntInRange(match[5], 0, 59)
	if !ok {
		return nil
	}
	second, ok := parseOptionalIntInRange(match[6], 0, 0, 59)
	if !ok {
		return nil
	}
	parsed := time.Date(year, month, day, applyMeridiem(hour, match[7]), minute, second, 0, time.Local)
	return &parsed
}

func monthFromName(name string) (time.Month, bool) {
	if len(name) < 3 {
		return 0, false
	}
	short := strings.ToLower(name[:3])
	for i, candidate := range shortMonthNames {
		if candidate == short {
			return time.Month(i + 1), true
		}
	}
	return 0, false
}

func resetYear(raw string) (int, bool) {
	if raw == "" {
		return time.Now().Year(), true
	}
	return parseIntInRange(raw, 1970, 9999)
}

func parseIntInRange(raw string, min, max int) (int, bool) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, false
	}
	return value, true
}

func parseOptionalIntInRange(raw string, fallback, min, max int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	return parseIntInRange(raw, min, max)
}

func applyMeridiem(hour int, raw string) int {
	switch strings.ToUpper(raw) {
	case "AM":
		if hour == 12 {
			return 0
		}
	case "PM":
		if hour < 12 {
			return hour + 12
		}
	}
	return hour
}
