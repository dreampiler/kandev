package usage

import (
	"sort"
	"time"
)

// MergeObserved folds windows an agent reported into a provider reading.
//
// It never converts a provider failure into a success: the caller keeps the
// error it already had, and the cache keeps the failure, Retry-After and backoff
// state it recorded. Only the returned value is decorated, so an observation
// fills in usage without ever being counted as a successful provider re-query.
func MergeObserved(
	api *ProviderUsage,
	apiErr error,
	reading ObservedReading,
	observed bool,
) (*ProviderUsage, bool) {
	if !observed || len(reading.Windows) == 0 {
		return api, false
	}
	if apiErr != nil || api == nil {
		// The provider did not answer. The observation stands on its own: its own
		// provider, its own windows, and the instant it was taken as the fetch
		// time, flagged stale so a client states it is not a live read.
		source := reading.Source
		if source == "" {
			source = defaultObservedSource
		}
		return &ProviderUsage{
			Provider:       reading.Provider,
			Windows:        reading.Windows,
			FetchedAt:      reading.ObservedAt,
			Stale:          true,
			Observed:       true,
			ObservedSource: source,
		}, true
	}
	return mergeObservedWindows(api, reading), true
}

// mergeObservedWindows keeps the provider's own reading and adds only what it did
// not report. For a window both sides report, the newer observation wins: an agent
// reports utilization as it happens, so a fresher observation supersedes a value
// fetched from the API earlier.
func mergeObservedWindows(api *ProviderUsage, reading ObservedReading) *ProviderUsage {
	merged := *api
	pending := make(map[string]UtilizationWindow, len(reading.Windows))
	for _, window := range reading.Windows {
		pending[window.Label] = window
	}
	merged.Windows = make([]UtilizationWindow, 0, len(api.Windows)+len(pending))
	for _, window := range api.Windows {
		observed, served := pending[window.Label]
		if !served {
			merged.Windows = append(merged.Windows, window)
			continue
		}
		// Both sides reported this window, so exactly one value is served: the
		// observation when it is newer, the provider's own otherwise.
		delete(pending, window.Label)
		if !observationSupersedes(reading.ObservedAt, api.FetchedAt) {
			merged.Windows = append(merged.Windows, window)
			continue
		}
		merged.Observed = true
		merged.Windows = append(merged.Windows, observed)
	}
	pendingKeys := make([]string, 0, len(pending))
	for k := range pending {
		pendingKeys = append(pendingKeys, k)
	}
	sort.Strings(pendingKeys)
	for _, k := range pendingKeys {
		merged.Windows = append(merged.Windows, pending[k])
		merged.Observed = true
	}
	if merged.Observed {
		if reading.Source != "" {
			merged.ObservedSource = reading.Source
		} else {
			merged.ObservedSource = defaultObservedSource
		}
	}
	return &merged
}

// observationSupersedes reports whether a reading observed at observedAt describes
// the account more recently than the provider reading fetched at apiFetchedAt. An
// unknown provider fetch time yields the observation, because there is nothing to
// compare it against.
func observationSupersedes(observedAt, apiFetchedAt time.Time) bool {
	if apiFetchedAt.IsZero() {
		return true
	}
	return observedAt.After(apiFetchedAt)
}
