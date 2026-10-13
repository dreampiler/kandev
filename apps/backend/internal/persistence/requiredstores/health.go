package requiredstores

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/db/dialect"
	"github.com/kandev/kandev/internal/system/maintenance"
)

const (
	defaultProbeInterval = 15 * time.Second
	// defaultProbeTimeout bounds one periodic probe. Two seconds was shorter
	// than the wait for a read connection while many agent sessions ran, so a
	// busy pool was reported as an unavailable database.
	defaultProbeTimeout = 5 * time.Second
	// runtimeFailureThreshold is how many consecutive periodic probes must
	// fail before the stores are recorded unavailable. A single slow probe
	// under load previously turned every stateful route into a 503.
	runtimeFailureThreshold = 3
	// writerPingTimeoutFactor scales probeTimeout for a writer ping when the
	// writer has not committed within the last probe window. SQLite has one
	// writer connection, so the ping queues behind every in-flight write; an
	// idle writer answers at once, and only a stuck one uses the full budget.
	writerPingTimeoutFactor = 3
	// writerBusyPingTimeout bounds the writer ping while the writer is
	// committing. The commits already prove the writer works; the ping only
	// has to catch a closed pool, which fails without queueing.
	writerBusyPingTimeout = time.Second
)

// Health probes the shared database and every table declared by the catalog.
// It owns the periodic probe lifecycle but delegates state storage to Tracker.
type Health struct {
	tracker *Tracker
	pool    *db.Pool
	log     *logger.Logger

	mu           sync.Mutex
	interval     time.Duration
	probeTimeout time.Duration
	cancel       context.CancelFunc
	done         chan struct{}
	// runtimeFailures counts consecutive failed periodic probes.
	runtimeFailures int
	// writerActivity reports the writer's most recent transaction commit.
	// Tests replace it; the default reads the SQLite writer commit hook.
	writerActivity     func() (time.Time, bool)
	logMu              sync.Mutex
	lastLoggedState    State
	lastLoggedStoreIDs []string
	hasLoggedState     bool
}

type probeFailureDiagnostic struct {
	stage       string
	elapsed     time.Duration
	errorClass  string
	storeID     string
	writerStats sql.DBStats
	readerStats sql.DBStats
	// consecutiveFailures is set for periodic probes only.
	consecutiveFailures int
}

// NewHealth creates a runtime health probe for a completed store tracker.
func NewHealth(tracker *Tracker, pool *db.Pool, log *logger.Logger) *Health {
	return &Health{
		tracker:        tracker,
		pool:           pool,
		log:            log,
		interval:       defaultProbeInterval,
		probeTimeout:   defaultProbeTimeout,
		writerActivity: poolWriterActivity(pool),
	}
}

func poolWriterActivity(pool *db.Pool) func() (time.Time, bool) {
	return func() (time.Time, bool) {
		if pool == nil || pool.Writer() == nil {
			return time.Time{}, false
		}
		return db.SQLiteWriterCommitActivity(pool.Writer().DB)
	}
}

// SetInterval changes the periodic interval. It is intended for tests and
// controlled embedders; callers must set it before Start.
func (h *Health) SetInterval(interval time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if interval > 0 {
		h.interval = interval
	}
}

// Check runs one synchronous probe. It returns an aggregate error when one or
// more stores are unavailable, while recording an independent result for each
// catalog entry.
func (h *Health) Check(ctx context.Context) error {
	_, err := h.check(ctx)
	return err
}

func (h *Health) check(ctx context.Context) (*probeFailureDiagnostic, error) {
	return h.probe(ctx, false)
}

// probe runs one probe. A buffered probe is the periodic runtime path: its
// ping and table failures are recorded only once runtimeFailureThreshold of
// them happen in a row. A missing pool is never buffered.
func (h *Health) probe(ctx context.Context, buffered bool) (*probeFailureDiagnostic, error) {
	if h == nil || h.tracker == nil {
		return nil, errors.New("required-store health tracker is unavailable")
	}
	if h.pool == nil || h.pool.Writer() == nil || h.pool.Reader() == nil {
		err := errors.New("database pool is unavailable")
		diagnostic := h.failureDiagnostic("pool", 0, err, "")
		return diagnostic, h.recordUnavailable(err)
	}
	diagnostic, probeErr := h.pingWriter(ctx)
	// The reader stage (reader ping and every table probe) starts its own
	// budget after the writer ping, so a writer queued behind real writes
	// cannot consume it.
	readerCtx, cancelReader := context.WithTimeout(ctx, h.probeTimeout)
	defer cancelReader()
	if probeErr == nil {
		diagnostic, probeErr = h.pingReader(readerCtx)
	}
	results := make([]error, len(h.tracker.catalog))
	for index, descriptor := range h.tracker.catalog {
		results[index] = probeErr
		if probeErr == nil {
			tableDiagnostic, tableErr := h.probeTablesObserved(readerCtx, descriptor)
			results[index] = tableErr
			if diagnostic == nil {
				diagnostic = tableDiagnostic
			}
		}
	}
	if pending, tolerated := h.bufferRuntimeFailure(buffered, results, diagnostic); tolerated {
		return diagnostic, pending
	}
	var failures []error
	for index, result := range results {
		if err := h.tracker.RecordProbe(h.tracker.catalog[index].ID, result); err != nil {
			failures = append(failures, err)
			continue
		}
		if result != nil {
			failures = append(failures, fmt.Errorf("%s: %w", h.tracker.catalog[index].ID, result))
		}
	}
	if len(failures) == 0 {
		h.logTransition()
		return nil, nil
	}
	h.logTransition()
	return diagnostic, errors.Join(failures...)
}

// bufferRuntimeFailure tracks consecutive periodic probe failures. It reports
// whether the failed results are still below runtimeFailureThreshold and must
// be left unrecorded, returning their joined error; a success resets the
// count. Strict probes are never buffered and do not touch the count.
func (h *Health) bufferRuntimeFailure(
	buffered bool,
	results []error,
	diagnostic *probeFailureDiagnostic,
) (pending error, tolerated bool) {
	if !buffered {
		return nil, false
	}
	var failures []error
	for index, result := range results {
		if result != nil {
			failures = append(failures, fmt.Errorf("%s: %w", h.tracker.catalog[index].ID, result))
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(failures) == 0 {
		h.runtimeFailures = 0
		return nil, false
	}
	h.runtimeFailures++
	if diagnostic != nil {
		diagnostic.consecutiveFailures = h.runtimeFailures
	}
	return errors.Join(failures...), h.runtimeFailures < runtimeFailureThreshold
}

// MarkUnavailable records a destructive database transition before the
// maintenance owner releases its admission lease. This keeps stateful
// requests fail-closed while the process waits for the required restart.
func (h *Health) MarkUnavailable() {
	if h == nil || h.tracker == nil {
		return
	}
	_ = h.recordUnavailable(errors.New("database maintenance requires restart"))
}

// checkRuntime runs a periodic probe when the database can be admitted. SQLite
// maintenance owns the same writer pool, so a busy maintenance lease defers
// the probe instead of turning bounded writer contention into an unhealthy
// state. A failed periodic probe is only logged until runtimeFailureThreshold
// consecutive probes fail. Startup callers continue to use Check, which
// remains strict, and MarkUnavailable stays immediate.
func (h *Health) checkRuntime(ctx context.Context) (deferred bool, err error) {
	deferred, _, err = h.checkRuntimeDetailed(ctx)
	return deferred, err
}

func (h *Health) checkRuntimeDetailed(
	ctx context.Context,
) (deferred bool, diagnostic *probeFailureDiagnostic, err error) {
	if h.isSQLite() {
		release, ok := maintenance.ForPool(h.pool).TryAcquire()
		if !ok {
			if h.log != nil {
				h.log.Debug("required persistence probe deferred during database maintenance")
			}
			return true, nil, nil
		}
		defer release()
	}
	diagnostic, err = h.probe(ctx, true)
	return false, diagnostic, err
}

func (h *Health) isSQLite() bool {
	return h.pool != nil && h.pool.Writer() != nil && h.pool.Writer().DriverName() == dialect.SQLite3
}

func (h *Health) pingReader(ctx context.Context) (*probeFailureDiagnostic, error) {
	started := time.Now()
	if err := h.pool.Reader().PingContext(ctx); err != nil {
		return h.failureDiagnostic("reader_ping", time.Since(started), err, ""), fmt.Errorf("reader ping failed: %w", err)
	}
	return nil, nil
}

// pingWriter checks the single writer connection. The ping queues behind
// every in-flight write, so under sustained agent load it can miss any fixed
// deadline while the database is healthy. A deadline is therefore not a
// failure when the writer committed within the last probe window: that is
// contention, not an outage. Any other error (a closed pool, a driver error)
// and a deadline without recent commits still fail the probe.
func (h *Health) pingWriter(ctx context.Context) (*probeFailureDiagnostic, error) {
	started := time.Now()
	timeout := h.probeTimeout * writerPingTimeoutFactor
	if h.writerCommittedSince(started.Add(-h.progressWindow())) {
		timeout = min(writerBusyPingTimeout, h.probeTimeout)
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	err := h.pool.Writer().PingContext(pingCtx)
	cancel()
	if err == nil {
		return nil, nil
	}
	elapsed := time.Since(started)
	diagnostic := h.failureDiagnostic("writer_ping", elapsed, err, "")
	if errors.Is(err, context.DeadlineExceeded) && h.writerCommittedSince(time.Now().Add(-h.progressWindow())) {
		if h.log != nil {
			h.log.Debug("required persistence writer ping queued behind committing writes",
				diagnostic.logFields()...)
		}
		return nil, nil
	}
	return diagnostic, fmt.Errorf("writer ping failed: %w", err)
}

// writerCommittedSince reports whether the writer committed a transaction at
// or after since.
func (h *Health) writerCommittedSince(since time.Time) bool {
	if h.writerActivity == nil {
		return false
	}
	last, ok := h.writerActivity()
	return ok && !last.Before(since)
}

// progressWindow is how recent a writer commit must be to count as progress:
// one probe interval.
func (h *Health) progressWindow() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.interval
}

// probeTables checks schema presence through the reader pool. Schema reads do
// not need the writer, and the reader pool keeps answering while the single
// writer connection is busy with real writes.
func (h *Health) probeTables(ctx context.Context, descriptor Descriptor) error {
	for _, table := range descriptor.RequiredTables {
		exists, err := db.TableExistsContext(ctx, h.pool.Reader(), table)
		if err != nil {
			return fmt.Errorf("table probe failed: %w", err)
		}
		if !exists {
			return fmt.Errorf("required table %q is missing", table)
		}
	}
	return nil
}

func (h *Health) probeTablesObserved(
	ctx context.Context,
	descriptor Descriptor,
) (*probeFailureDiagnostic, error) {
	started := time.Now()
	err := h.probeTables(ctx, descriptor)
	if err == nil {
		return nil, nil
	}
	return h.failureDiagnostic("table_probe", time.Since(started), err, descriptor.ID), err
}

func (h *Health) failureDiagnostic(stage string, elapsed time.Duration, err error, storeID string) *probeFailureDiagnostic {
	diagnostic := &probeFailureDiagnostic{
		stage:      stage,
		elapsed:    elapsed,
		errorClass: classifyProbeError(err),
		storeID:    storeID,
	}
	if h.pool == nil {
		return diagnostic
	}
	if writer := h.pool.Writer(); writer != nil {
		diagnostic.writerStats = writer.Stats()
	}
	if reader := h.pool.Reader(); reader != nil {
		diagnostic.readerStats = reader.Stats()
	}
	return diagnostic
}

func classifyProbeError(err error) string {
	message := ""
	if err != nil {
		message = strings.ToLower(err.Error())
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case strings.Contains(message, "required table") && strings.Contains(message, "missing"):
		return "required_table_missing"
	default:
		return "probe_error"
	}
}

func (d *probeFailureDiagnostic) logFields() []zap.Field {
	if d == nil {
		return []zap.Field{zap.String("stage", "unknown"), zap.String("error_class", "probe_error")}
	}
	fields := []zap.Field{
		zap.String("stage", d.stage),
		zap.Float64("elapsed_ms", float64(d.elapsed)/float64(time.Millisecond)),
		zap.String("error_class", d.errorClass),
		zap.Int("writer_open_connections", d.writerStats.OpenConnections),
		zap.Int("writer_in_use", d.writerStats.InUse),
		zap.Int64("writer_wait_count", d.writerStats.WaitCount),
		zap.Float64("writer_wait_duration_ms", float64(d.writerStats.WaitDuration)/float64(time.Millisecond)),
		zap.Int("reader_open_connections", d.readerStats.OpenConnections),
		zap.Int("reader_in_use", d.readerStats.InUse),
		zap.Int64("reader_wait_count", d.readerStats.WaitCount),
		zap.Float64("reader_wait_duration_ms", float64(d.readerStats.WaitDuration)/float64(time.Millisecond)),
	}
	if d.storeID != "" {
		fields = append(fields, zap.String("store_id", d.storeID))
	}
	if d.consecutiveFailures > 0 {
		fields = append(fields,
			zap.Int("consecutive_failures", d.consecutiveFailures),
			zap.Int("failure_threshold", runtimeFailureThreshold))
	}
	return fields
}

func (h *Health) logProbeFailure(diagnostic *probeFailureDiagnostic) {
	if h == nil || h.log == nil {
		return
	}
	h.log.Warn("required persistence probe failed", diagnostic.logFields()...)
}

func (h *Health) recordUnavailable(err error) error {
	for _, descriptor := range h.tracker.catalog {
		if recordErr := h.tracker.RecordProbe(descriptor.ID, err); recordErr != nil {
			return recordErr
		}
	}
	h.logTransition()
	return err
}

// Start begins the periodic probe loop. The returned cleanup is idempotent.
func (h *Health) Start(ctx context.Context) func() error {
	if h == nil {
		return func() error { return nil }
	}
	h.mu.Lock()
	if h.cancel != nil {
		h.mu.Unlock()
		return h.Stop
	}
	loopCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.done = make(chan struct{})
	interval := h.interval
	done := h.done
	h.mu.Unlock()
	go h.run(loopCtx, interval, done)
	return h.Stop
}

func (h *Health) run(ctx context.Context, interval time.Duration, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			// Each stage bounds itself (writer ping, reader stage); this cap
			// only guarantees the loop never waits longer than both together.
			checkCtx, cancel := context.WithTimeout(ctx, h.probeTimeout*(writerPingTimeoutFactor+1))
			_, diagnostic, err := h.checkRuntimeDetailed(checkCtx)
			if err != nil {
				h.logProbeFailure(diagnostic)
			}
			cancel()
		}
	}
}

// Stop terminates the periodic probe loop.
func (h *Health) Stop() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	cancel := h.cancel
	done := h.done
	h.cancel = nil
	h.done = nil
	h.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	<-done
	return nil
}

// Healthy reports the current aggregate runtime state.
func (h *Health) Healthy() bool {
	return h != nil && h.tracker != nil && h.tracker.Healthy()
}

// State returns the current aggregate state.
func (h *Health) State() State {
	if h == nil || h.tracker == nil {
		return StateUnhealthy
	}
	return h.tracker.AggregateState()
}

// UnhealthyStoreIDs returns the stable list used by readiness and caller
// errors.
func (h *Health) UnhealthyStoreIDs() []string {
	if h == nil || h.tracker == nil {
		return nil
	}
	return h.tracker.UnhealthyStoreIDs()
}

// UnavailableStoreIDs returns all stores that are not currently healthy.
func (h *Health) UnavailableStoreIDs() []string {
	if h == nil || h.tracker == nil {
		return nil
	}
	return h.tracker.UnavailableStoreIDs()
}

func (h *Health) logTransition() {
	if h.log == nil {
		return
	}
	h.logMu.Lock()
	state, storeIDs := healthTransitionSnapshot(h.tracker)
	h.mu.Lock()
	if h.hasLoggedState && state == h.lastLoggedState && slices.Equal(storeIDs, h.lastLoggedStoreIDs) {
		h.mu.Unlock()
		h.logMu.Unlock()
		return
	}
	h.lastLoggedState = state
	h.lastLoggedStoreIDs = append(h.lastLoggedStoreIDs[:0], storeIDs...)
	h.hasLoggedState = true
	h.mu.Unlock()
	h.log.Info("required persistence state updated",
		zap.String("state", string(state)),
		zap.Strings("store_ids", storeIDs),
		zap.String("error_class", publicErrorClass(state)))
	h.logMu.Unlock()
}

func healthTransitionSnapshot(tracker *Tracker) (State, []string) {
	if tracker == nil {
		return StateUnhealthy, nil
	}
	statuses := tracker.Snapshot()
	state := StateHealthy
	storeIDs := make([]string, 0)
	for _, status := range statuses {
		if status.State == StateUnhealthy {
			storeIDs = append(storeIDs, status.ID)
		}
		if state == StateHealthy {
			switch status.State {
			case StateInitializing:
				state = StateInitializing
			case StateUnhealthy:
				state = StateUnhealthy
			}
		}
	}
	sort.Strings(storeIDs)
	return state, storeIDs
}

// PublicError returns a stable, non-sensitive error for diagnostics.
func PublicError(status Status) string {
	if status.Error == "" {
		return ""
	}
	if strings.Contains(status.Error, "required table") && strings.Contains(status.Error, "missing") {
		return status.Error
	}
	return "database probe failed"
}

func publicErrorClass(state State) string {
	if state == StateUnhealthy {
		return "database_probe"
	}
	return "none"
}
