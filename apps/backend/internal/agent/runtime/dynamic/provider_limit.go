package dynamic

import (
	"strings"
	"time"
)

// ProviderLimit is the operator-entered limit setting of one provider, keyed by
// the provider prefix of its model IDs.
type ProviderLimit struct {
	Provider string
	// MonthlyResetAt is any one occurrence of the provider's monthly reset. The
	// reset repeats on the same day of month and clock time in
	// MonthlyResetTimezone, clamped to the last day of shorter months.
	MonthlyResetAt       *time.Time
	MonthlyResetTimezone string
	// BlockUntil pauses every paid model of the provider until that instant.
	BlockUntil *time.Time
	UpdatedAt  time.Time
}

// NormalizeProvider returns the stored form of a provider key.
func NormalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(provider), "/")))
}

// NextMonthlyReset returns the first monthly reset strictly after now.
func (l ProviderLimit) NextMonthlyReset(now time.Time) (time.Time, bool) {
	if l.MonthlyResetAt == nil || l.MonthlyResetAt.IsZero() {
		return time.Time{}, false
	}
	location := time.UTC
	if l.MonthlyResetTimezone != "" {
		if loaded, err := time.LoadLocation(l.MonthlyResetTimezone); err == nil {
			location = loaded
		}
	}
	anchor := l.MonthlyResetAt.In(location)
	local := now.In(location)
	offset := (local.Year()-anchor.Year())*12 + int(local.Month()-anchor.Month())
	next := monthlyOccurrence(anchor, offset)
	if !next.After(now) {
		next = monthlyOccurrence(anchor, offset+1)
	}
	return next, true
}

// BlockedUntil returns the operator block covering a model of this provider.
// Free models keep their own limits and are never covered.
func (l ProviderLimit) BlockedUntil(modelID string, now time.Time) (time.Time, bool) {
	return l.BlockedUntilCandidate(Candidate{ModelID: modelID}, now)
}

// BlockedUntilCandidate returns the operator block covering one candidate. A
// free candidate keeps its own limits and is never covered, so a block an
// operator entered for a provider's paid models never pauses a free model of the
// same provider.
func (l ProviderLimit) BlockedUntilCandidate(candidate Candidate, now time.Time) (time.Time, bool) {
	if l.BlockUntil == nil || !l.BlockUntil.After(now) || freeCandidate(candidate) {
		return time.Time{}, false
	}
	return *l.BlockUntil, true
}

func monthlyOccurrence(anchor time.Time, monthOffset int) time.Time {
	firstOfMonth := time.Date(anchor.Year(), anchor.Month()+time.Month(monthOffset), 1, 0, 0, 0, 0, anchor.Location())
	lastDay := firstOfMonth.AddDate(0, 1, -1).Day()
	day := min(anchor.Day(), lastDay)
	return time.Date(firstOfMonth.Year(), firstOfMonth.Month(), day,
		anchor.Hour(), anchor.Minute(), anchor.Second(), 0, anchor.Location())
}
