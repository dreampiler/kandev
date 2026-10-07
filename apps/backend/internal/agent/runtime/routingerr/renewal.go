package routingerr

import (
	"regexp"
	"time"
)

// renewalDatePattern reads the plan renewal day an exhausted-credit notice
// states, such as DevPass's "Upgrade your plan or wait for renewal on
// 10/7/2026". The slash form is the provider's month/day/year order; the ISO
// form is accepted for providers that state the day unambiguously.
var renewalDatePattern = regexp.MustCompile(
	`(?i)\brenew(?:al|s)?\s+on\s+(?:(\d{1,2})/(\d{1,2})/(\d{4})|(\d{4})-(\d{2})-(\d{2}))\b`,
)

// earliestDayZone is the zone in which every calendar day begins first.
var earliestDayZone = time.FixedZone("UTC+14", 14*60*60)

// parseRenewalDayStart returns the earliest instant the stated renewal day can
// have begun anywhere. The notice names a day but no zone, so the earliest
// reading is the only one that can bound a suspension without risking a block
// that outlasts the provider's own renewal.
func parseRenewalDayStart(text string) *time.Time {
	match := renewalDatePattern.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	monthRaw, dayRaw, yearRaw := match[1], match[2], match[3]
	if yearRaw == "" {
		yearRaw, monthRaw, dayRaw = match[4], match[5], match[6]
	}
	year, ok := parseIntInRange(yearRaw, 1970, 9999)
	if !ok {
		return nil
	}
	month, ok := parseIntInRange(monthRaw, 1, 12)
	if !ok {
		return nil
	}
	day, ok := parseIntInRange(dayRaw, 1, 31)
	if !ok {
		return nil
	}
	start, ok := makeResetTime(year, time.Month(month), day, 0, 0, 0, earliestDayZone)
	if !ok {
		return nil
	}
	return &start
}
