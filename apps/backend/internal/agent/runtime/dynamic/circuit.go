package dynamic

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type ResourceScope string

const (
	ScopeProvider   ResourceScope = "provider"
	ScopeCredential ResourceScope = "credential"
	ScopeModel      ResourceScope = "model"
	ScopeProfile    ResourceScope = "profile"
)

func ResourceKey(scope ResourceScope, fingerprint string) string {
	return string(scope) + ":" + fingerprint
}

type circuit struct {
	state      CircuitState
	until      time.Time
	code       routingerr.Code
	probeUntil time.Time
	// strikes counts consecutive suspensions. Only a recorded success clears
	// it, so a probe launch that starts but then hits the limit again still
	// moves the next suspension up its ladder.
	strikes int
}

// CircuitSnapshot is the durable representation of one resource circuit.
// Keys are opaque fingerprints and must already be safe to persist.
type CircuitSnapshot struct {
	Key        string
	State      CircuitState
	Until      time.Time
	Code       routingerr.Code
	ProbeUntil time.Time
	Strikes    int
}

// CircuitPersistence stores shared resource health across backend restarts.
type CircuitPersistence interface {
	SaveCircuit(context.Context, CircuitSnapshot) error
	LoadCircuits(context.Context) ([]CircuitSnapshot, error)
}

type CircuitRegistryOption func(*CircuitRegistry)

func WithCircuitClock(now func() time.Time) CircuitRegistryOption {
	return func(registry *CircuitRegistry) {
		if now != nil {
			registry.now = now
		}
	}
}

func WithCircuitPersistence(persistence CircuitPersistence) CircuitRegistryOption {
	return func(registry *CircuitRegistry) { registry.persist = persistence }
}

// WithCircuitLogger sets the logger used to report a failed durable write.
// Defaults to a no-op logger.
func WithCircuitLogger(log *zap.Logger) CircuitRegistryOption {
	return func(registry *CircuitRegistry) {
		if log != nil {
			registry.logger = log
		}
	}
}

type CircuitRegistry struct {
	mu       sync.Mutex
	now      func() time.Time
	circuits map[string]circuit
	persist  CircuitPersistence
	logger   *zap.Logger
	pending  map[string]struct{}
}

func NewCircuitRegistry(options ...CircuitRegistryOption) *CircuitRegistry {
	registry := &CircuitRegistry{
		now:      time.Now,
		circuits: make(map[string]circuit),
		logger:   zap.NewNop(),
		pending:  make(map[string]struct{}),
	}
	for _, option := range options {
		option(registry)
	}
	return registry
}

// Restore loads durable circuit state before routing workers start.
func (r *CircuitRegistry) Restore(ctx context.Context) error {
	if r.persist == nil {
		return nil
	}
	snapshots, err := r.persist.LoadCircuits(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, snapshot := range snapshots {
		r.circuits[snapshot.Key] = circuit{
			state: snapshot.State, until: snapshot.Until,
			code: snapshot.Code, probeUntil: snapshot.ProbeUntil,
			strikes: snapshot.Strikes,
		}
	}
	return nil
}

func (r *CircuitRegistry) Open(key string, until time.Time, code routingerr.Code) {
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushPendingLocked(key)
	previous := r.circuits[key]
	strikes := previous.strikes + 1
	if previous.state == CircuitOpen && r.now().Before(previous.until) {
		// Another attempt hit the same running suspension: it is the same
		// incident, so it does not count again.
		strikes = previous.strikes
	}
	r.circuits[key] = circuit{state: CircuitOpen, until: until, code: code, strikes: strikes}
	_ = r.persistSnapshotLocked(key)
	r.logger.Info("dynamic resource suspended",
		zap.String("key", key),
		zap.String("code", string(code)),
		zap.Time("until", until),
		zap.Int("strikes", strikes),
	)
}

// NextStrike returns the strike number a failure recorded now would carry.
func (r *CircuitRegistry) NextStrike(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.circuits[key]
	if entry.state == CircuitOpen && r.now().Before(entry.until) {
		return entry.strikes
	}
	return entry.strikes + 1
}

// RecordSuccess clears the strike count after the resource produced real
// output. An expired or probing circuit closes as well, because the output is
// stronger evidence than the probe launch. A circuit whose block is still
// running stays open: a concurrent failure reopened it after this output.
func (r *CircuitRegistry) RecordSuccess(key string) {
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.circuits[key]
	if !ok {
		return
	}
	now := r.now()
	if entry.state == CircuitOpen && now.Before(entry.until) {
		return
	}
	if entry.state == CircuitClosed && entry.strikes == 0 {
		return
	}
	r.flushPendingLocked(key)
	r.circuits[key] = circuit{state: CircuitClosed}
	_ = r.persistSnapshotLocked(key)
	r.logger.Info("dynamic resource suspension cleared after output",
		zap.String("key", key),
		zap.Int("previous_strikes", entry.strikes),
	)
}

// ResourceState is the read-only health of one resource circuit.
type ResourceState string

const (
	// ResourceAvailable means no suspension applies.
	ResourceAvailable ResourceState = "none"
	// ResourceWaiting means a suspension is running until Until.
	ResourceWaiting ResourceState = "waiting"
	// ResourceExpired means the suspension ended and the next selection may
	// claim the probe.
	ResourceExpired ResourceState = "expired"
	// ResourceProbing means another selection holds the probe until Until.
	ResourceProbing ResourceState = "probing"
)

// ResourceStatus describes one circuit without mutating it.
type ResourceStatus struct {
	State   ResourceState
	Until   time.Time
	Code    routingerr.Code
	Strikes int
}

// Inspect reports a circuit's health at now. It takes no probe lease, so a
// preview can tell an expired suspension from a running one while IsOpen keeps
// answering open for both until a selection claims the probe.
func (r *CircuitRegistry) Inspect(key string, now time.Time) ResourceStatus {
	if key == "" {
		return ResourceStatus{State: ResourceAvailable}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.circuits[key]
	if !ok || entry.state == CircuitClosed {
		return ResourceStatus{State: ResourceAvailable, Strikes: entry.strikes}
	}
	status := ResourceStatus{Code: entry.code, Strikes: entry.strikes}
	switch {
	case entry.state == CircuitHalfOpen && now.Before(entry.probeUntil):
		status.State, status.Until = ResourceProbing, entry.probeUntil
	case now.Before(entry.until):
		status.State, status.Until = ResourceWaiting, entry.until
	default:
		status.State, status.Until = ResourceExpired, entry.until
	}
	return status
}

func (r *CircuitRegistry) IsOpen(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.circuits[key]
	if !ok || entry.state == CircuitClosed {
		return false
	}
	if entry.state == CircuitHalfOpen {
		return true
	}
	// An expired open circuit remains unavailable until one caller acquires
	// the exclusive probe lease. Treating it as closed here would let every
	// selector stampede the provider between expiry and probe acquisition.
	return entry.state == CircuitOpen && !entry.until.IsZero()
}

type ProbeLease struct {
	Key       string
	ExpiresAt time.Time
}

func (r *CircuitRegistry) AcquireProbe(key string, duration time.Duration) (ProbeLease, bool) {
	if key == "" || duration <= 0 {
		return ProbeLease{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	entry, ok := r.circuits[key]
	if !ok || entry.state == CircuitClosed || now.Before(entry.until) || now.Before(entry.probeUntil) {
		return ProbeLease{}, false
	}
	r.flushPendingLocked(key)
	lease := ProbeLease{Key: key, ExpiresAt: now.Add(duration)}
	entry.state = CircuitHalfOpen
	entry.probeUntil = lease.ExpiresAt
	r.circuits[key] = entry
	_ = r.persistSnapshotLocked(key)
	return lease, true
}

func (r *CircuitRegistry) ReleaseProbe(lease ProbeLease, success bool, backoff time.Duration) {
	if lease.Key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.circuits[lease.Key]
	if !ok || entry.state != CircuitHalfOpen || !entry.probeUntil.Equal(lease.ExpiresAt) {
		return
	}
	r.flushPendingLocked(lease.Key)
	entry.probeUntil = time.Time{}
	if success {
		// A successful launch only proves the process started. The strike
		// count survives until RecordSuccess sees real output.
		entry.state = CircuitClosed
		entry.until = time.Time{}
		entry.code = ""
	} else {
		entry.state = CircuitOpen
		entry.until = r.now().Add(backoff)
	}
	r.circuits[lease.Key] = entry
	_ = r.persistSnapshotLocked(lease.Key)
}

// pendingFlushBudget bounds how many other pending keys a single mutation
// retries. r.mu also gates IsOpen on the routing hot path, and each retry is
// a blocking SaveCircuit call, so a mutation during a sustained persistence
// outage must hold the lock for at most one extra write, not one per pending
// key: unbounded fan-out both grows total SaveCircuit calls quadratically
// across mutations and holds the routing mutex for the sum of every pending
// write's duration.
const pendingFlushBudget = 1

// flushPendingLocked retries the durable write for up to pendingFlushBudget
// keys whose previous SaveCircuit failed, using each key's current in-memory
// snapshot. Called from every mutator so a transient persistence failure
// self-heals across subsequent circuit events instead of leaving the durable
// row permanently behind memory.
func (r *CircuitRegistry) flushPendingLocked(skipKey string) {
	flushed := 0
	for key := range r.pending {
		if key == skipKey {
			continue
		}
		if flushed >= pendingFlushBudget {
			return
		}
		_ = r.persistSnapshotLocked(key)
		flushed++
	}
}

// persistSnapshotLocked writes key's current snapshot and returns the
// persistence error, if any. A failure is logged at WARN once per failure
// streak (not once per retry) and the key is recorded in pending so a later
// mutation retries the write; a caller-visible error here would either fail
// routing decisions that must still complete (Open) or make every candidate
// unselectable while the store is down (AcquireProbe), so callers do not act
// on the return value directly.
func (r *CircuitRegistry) persistSnapshotLocked(key string) error {
	if r.persist == nil {
		return nil
	}
	entry, ok := r.circuits[key]
	if !ok {
		return nil
	}
	err := r.persist.SaveCircuit(context.Background(), CircuitSnapshot{
		Key: key, State: entry.state, Until: entry.until,
		Code: entry.code, ProbeUntil: entry.probeUntil, Strikes: entry.strikes,
	})
	if err != nil {
		_, alreadyPending := r.pending[key]
		r.pending[key] = struct{}{}
		if !alreadyPending {
			r.logger.Warn("failed to persist circuit snapshot",
				zap.String("key", key),
				zap.String("state", string(entry.state)),
				zap.Time("until", entry.until),
				zap.Error(err),
			)
		}
		return err
	}
	delete(r.pending, key)
	return nil
}
