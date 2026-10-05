package usage

import (
	"encoding/json"
	"sync"
	"time"
)

// observedMaxAge bounds how long an observation stays usable. An agent reports
// its windows while it runs, so a reading older than this describes a session
// Kandev is no longer observing rather than the account's current state.
const observedMaxAge = 24 * time.Hour

// observedPersistencePrefix namespaces an account's stored observation. The
// account part is the cache key the usage cache already uses to group profiles
// on one account, so no credential path is written and no second identity is
// derived for the same account.
const observedPersistencePrefix = "usage.observed.v1."

// ObservedWindow is one subscription window an agent reported while running.
// Utilization is the fraction of the window the provider has consumed (0–1), not
// a percentage.
type ObservedWindow struct {
	Provider    string    `json:"provider"`
	WindowType  string    `json:"window_type"`
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at,omitempty"`
	Status      string    `json:"status,omitempty"`
	Overage     bool      `json:"overage,omitempty"`
	ObservedAt  time.Time `json:"observed_at"`
}

// ObservedReading is the usable observation for one account: the windows that
// still describe a real window, in the shape every usage consumer already reads.
type ObservedReading struct {
	Provider   string
	Windows    []UtilizationWindow
	ObservedAt time.Time
}

// ObservedPersistence stores one account's observation between processes. It is
// best-effort: the in-memory value stays authoritative when a write fails.
type ObservedPersistence interface {
	Load(key string) ([]byte, bool, error)
	Save(key string, value []byte) error
}

// observedAccount is one account's newest observation per window type.
type observedAccount struct {
	loaded     bool
	windows    map[string]ObservedWindow
	provider   string
	observedAt time.Time
}

// ObservedStore holds the windows agents reported for accounts whose provider
// usage API cannot answer. It is keyed by the same cache key the usage cache
// groups accounts with, so profiles that already share one cached read also share
// one observation.
type ObservedStore struct {
	mu          sync.Mutex
	accounts    map[string]*observedAccount
	persistence ObservedPersistence
	now         func() time.Time
}

// NewObservedStore creates a store. A nil persistence keeps observations in
// memory only, which is the whole feature while the process runs.
func NewObservedStore(persistence ObservedPersistence) *ObservedStore {
	return &ObservedStore{
		accounts:    make(map[string]*observedAccount),
		persistence: persistence,
		now:         time.Now,
	}
}

// SetClockForTest overrides the time source without sleeping.
func (s *ObservedStore) SetClockForTest(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Record stores the newest observation for a window type. An older observation
// for a type already known is ignored, so a replayed frame cannot move a reading
// backwards. The persistence error is returned for the caller to report; the
// in-memory value is already stored either way.
func (s *ObservedStore) Record(accountKey string, window ObservedWindow) error {
	if accountKey == "" || window.WindowType == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account := s.account(accountKey)
	if existing, seen := account.windows[window.WindowType]; seen &&
		!window.ObservedAt.After(existing.ObservedAt) {
		return nil
	}
	account.windows[window.WindowType] = window
	if window.Provider != "" {
		account.provider = window.Provider
	}
	if window.ObservedAt.After(account.observedAt) {
		account.observedAt = window.ObservedAt
	}
	return s.persist(accountKey, account)
}

// Latest returns the account's usable windows, or false when it has none. A
// window past its own reset, or older than the observation bound, no longer
// describes a window that exists; an overage observation is not a subscription
// window fraction and is never served.
func (s *ObservedStore) Latest(accountKey string) (ObservedReading, bool) {
	if accountKey == "" {
		return ObservedReading{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account := s.account(accountKey)
	now := s.timeNow()
	reading := ObservedReading{Provider: account.provider, ObservedAt: account.observedAt}
	for _, window := range account.windows {
		served, ok := servedObservedWindow(window, now)
		if !ok {
			continue
		}
		reading.Windows = append(reading.Windows, served)
		if window.ObservedAt.After(reading.ObservedAt) {
			reading.ObservedAt = window.ObservedAt
		}
	}
	if len(reading.Windows) == 0 {
		return ObservedReading{}, false
	}
	return reading, true
}

// account returns the account record, loading its persisted observation once.
func (s *ObservedStore) account(accountKey string) *observedAccount {
	account, seen := s.accounts[accountKey]
	if !seen {
		account = &observedAccount{windows: make(map[string]ObservedWindow)}
		s.accounts[accountKey] = account
	}
	if account.loaded {
		return account
	}
	account.loaded = true
	if s.persistence == nil {
		return account
	}
	raw, found, err := s.persistence.Load(observedPersistencePrefix + accountKey)
	if err != nil || !found || len(raw) == 0 {
		return account
	}
	var stored observedSnapshot
	if err := json.Unmarshal(raw, &stored); err != nil {
		return account
	}
	for _, window := range stored.Windows {
		if _, known := account.windows[window.WindowType]; !known {
			account.windows[window.WindowType] = window
		}
	}
	account.provider = stored.Provider
	if stored.ObservedAt.After(account.observedAt) {
		account.observedAt = stored.ObservedAt
	}
	return account
}

// persist writes the account's snapshot. Failure is returned rather than
// swallowed so the caller can report it once per interval.
func (s *ObservedStore) persist(accountKey string, account *observedAccount) error {
	if s.persistence == nil {
		return nil
	}
	snapshot := observedSnapshot{Provider: account.provider, ObservedAt: account.observedAt}
	for _, window := range account.windows {
		snapshot.Windows = append(snapshot.Windows, window)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return s.persistence.Save(observedPersistencePrefix+accountKey, raw)
}

// observedSnapshot is the persisted form of one account's observation.
type observedSnapshot struct {
	Provider   string           `json:"provider,omitempty"`
	Windows    []ObservedWindow `json:"windows"`
	ObservedAt time.Time        `json:"observed_at"`
}

func (s *ObservedStore) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// servedObservedWindow converts one observation into the window every usage
// consumer already reads, or reports that it is not servable.
func servedObservedWindow(window ObservedWindow, now time.Time) (UtilizationWindow, bool) {
	if window.Overage {
		return UtilizationWindow{}, false
	}
	label, duration, modelScoped, ok := observedWindowShape(window.WindowType)
	if !ok {
		return UtilizationWindow{}, false
	}
	if now.Sub(window.ObservedAt) > observedMaxAge {
		return UtilizationWindow{}, false
	}
	if !window.ResetsAt.IsZero() && !window.ResetsAt.After(now) {
		return UtilizationWindow{}, false
	}
	served := UtilizationWindow{
		Label:               label,
		UtilizationPct:      window.Utilization * 100,
		ResetAt:             window.ResetsAt,
		DurationSeconds:     int64(duration / time.Second),
		AmbiguousModelScope: modelScoped,
		LimitReached:        window.Status == observedStatusRejected,
	}
	if !window.ResetsAt.IsZero() {
		served.StartAt = window.ResetsAt.Add(-duration)
	}
	return served, true
}

// observedStatusRejected is the one observed status that means the window has no
// remaining capacity, which UtilizationWindow.Exhausted already honors.
const observedStatusRejected = "rejected"

// observedWindowShape maps an observed window kind to the label and length the
// provider's own API path already uses for it, plus whether the provider scoped
// it to a model it did not identify. A per-model weekly window names its model
// for display only, so it stays unusable for a pace score rather than being
// matched to a candidate. An unknown kind has no length and is not served.
func observedWindowShape(windowType string) (string, time.Duration, bool, bool) {
	switch windowType {
	case "five_hour":
		return claudeLabel5Hour, 5 * time.Hour, false, true
	case "seven_day", "seven_day_overage_included":
		return claudeLabel7Day, 7 * 24 * time.Hour, false, true
	case "seven_day_opus", "seven_day_sonnet":
		return claudeLabel7Day, 7 * 24 * time.Hour, true, true
	default:
		return "", 0, false, false
	}
}
