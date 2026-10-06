package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/common/constants"
	"github.com/kandev/kandev/internal/task/models"
)

// reservationExpiryAllowance is the slice of the backstop that follows the session's
// STARTING write: the start deadline inside the launch goroutine. The preparation
// phase before it is already bounded by constants.AgentLaunchTimeout, which is read
// rather than copied so this tracks an operator-raised preparation budget.
const reservationExpiryAllowance = 5 * time.Minute

// launchOrigin is the explicit automatic/manual classification threaded from the
// caller into the admission controller. It is never inferred from the transport or
// the handler, both of which carry human clicks and server-initiated automation.
type launchOrigin string

const (
	launchOriginAutomatic launchOrigin = "automatic"
	launchOriginManual    launchOrigin = "manual"
)

// ceilingFieldReasonCode and ceilingFieldCeiling name the log field and card
// metadata key shared across every ceiling log line, message row and audit
// record, so the reason code and the ceiling value are always found under
// the same name regardless of which call site wrote them.
const (
	ceilingFieldReasonCode = "reason_code"
	ceilingFieldCeiling    = "ceiling"
	// The class fields are a closed, low-cardinality set: a lane name and two
	// counts. No task, session or agent identifier is ever one of them.
	ceilingFieldClass           = "class"
	ceilingFieldClassCeiling    = "class_ceiling"
	ceilingFieldClassPopulation = "class_population"
	// The precedence fields name the queued launch a new automatic launch
	// yielded to, plus the closed state that made it rank first. They appear
	// only on a deferred-precedence refusal.
	ceilingFieldPrecedesTask    = "precedes_task"
	ceilingFieldPrecedesSession = "precedes_session"
	ceilingFieldPrecedesKind    = "precedes_kind"
	ceilingFieldPrecedesState   = "precedes_state"
)

// Reason codes carried verbatim by the admission log line and the card surface.
const (
	ceilingReasonRefused           = "ceiling"
	ceilingReasonControlRefused    = "ceiling_control"
	ceilingReasonManualOverride    = "ceiling_manual_override"
	ceilingReasonUnknownPopulation = "ceiling_unknown_population"
	ceilingReasonSuperseded        = "ceiling_superseded"
	ceilingReasonDeferWriteFailed  = "ceiling_defer_write_failed"
	// ceilingReasonDroppedTaskIneligible covers AC-17b(a)-(d): the task is
	// archived, cancelled, names a session that no longer exists, or moved
	// to a step that no longer auto-starts.
	ceilingReasonDroppedTaskIneligible = "ceiling_dropped_task_ineligible"
	// ceilingReasonDroppedLaunchGateDeclined is AC-17b(e): the launch's own
	// precondition (today, the terminal-PR guard) declined it at replay time.
	ceilingReasonDroppedLaunchGateDeclined = "ceiling_dropped_launch_gate_declined"
	// ceilingReasonDroppedUnreplayableRecord is AC-17b(f): the record's own
	// ceiling_launch_kind is absent or outside the closed set.
	ceilingReasonDroppedUnreplayableRecord = "ceiling_dropped_unreplayable_record"
	// ceilingReasonSurfaceWriteFailed is AC-49g: the deferral itself persisted
	// successfully, but the card note describing it could not be written.
	ceilingReasonSurfaceWriteFailed = "ceiling_surface_write_failed"
	// ceilingReasonDeferredPrecedes is a refusal that had free capacity: the
	// lane was not saturated, and the slot was reserved for a launch that was
	// already queued. It is distinct from ceiling/ceiling_control so an
	// operator reading a card or a log line can tell a full instance from a
	// queue ordering decision.
	ceilingReasonDeferredPrecedes = "ceiling_deferred_precedes"
)

// deferredPrecedenceState is the closed set describing why a queued launch
// ranked ahead of a new automatic one. A queued launch that is already being
// dispatched claims no priority and is not selected at all.
const (
	deferredPrecedesEligible        = "eligible"
	deferredPrecedesListUnavailable = "list_unavailable"
)

// deferredPrecedence is the read-only answer a launch seam supplies when the
// admission controller asks whether a queued launch outranks this request.
type deferredPrecedence struct {
	precedes  bool
	taskID    string
	sessionID string
	kind      models.CeilingLaunchKind
	state     string
}

// ceilingClass is the admission lane a session is counted against. The lane is
// derived from durable data (the session's stored agent profile) by this
// package alone; a launch caller supplies the profile it already resolved and
// never the class itself.
type ceilingClass string

const (
	// ceilingClassWorker is every session that is not one of the operator's
	// configured control profiles. It is also the class an unresolvable profile
	// falls into, so an unknown session can never take capacity reserved for
	// monitors.
	ceilingClassWorker ceilingClass = "worker"
	// ceilingClassControl is a session whose agent profile is in the configured
	// control profile set. It is admitted against its own ceiling, which is not
	// drawn from the worker ceiling.
	ceilingClassControl ceilingClass = "control"
)

// admittedSessionLister supplies the persisted half of the population. It returns
// refs rather than a count because the population unions rows with reservations by
// session id, so a session holding both is counted once.
type admittedSessionLister interface {
	ListAdmittedSessionRefs(ctx context.Context) ([]models.AdmittedSessionRef, error)
}

// ceilingReservation is an in-flight admission held until the session is observed
// running or the launch fails. sessionID is empty while the reservation is still
// keyed by a launch-scoped identifier, which is the window between admission at
// seam 1 and the session's creation. class is fixed at admission so the
// launch-to-session window counts against the lane it was admitted into.
type ceilingReservation struct {
	sessionID string
	class     ceilingClass
	takenAt   time.Time
}

// admissionRequest is one launch asking for capacity.
type admissionRequest struct {
	taskID    string
	sessionID string
	origin    launchOrigin
	seam      string
	// agentProfileID is the profile this launch resolved for itself. It is the
	// only classification input; an empty value is read as worker.
	agentProfileID  string
	yieldToDeferred func(context.Context, ceilingClass) deferredPrecedence
}

// admissionDecision is the controller's answer.
type admissionDecision struct {
	admitted        bool
	manualOverride  bool
	reservationKey  string
	population      int
	populationKnown bool
	ceiling         int
	reasonCode      string
	handedOff       bool
	class           ceilingClass
	classPopulation int
	classCeiling    int
	// The precedence fields describe the queued launch this request yielded
	// to. They are set only for ceilingReasonDeferredPrecedes.
	precedesTaskID    string
	precedesSessionID string
	precedesKind      models.CeilingLaunchKind
	precedesState     string
}

// SessionCeilingObservation is a point-in-time view of the admission
// controller. It is deliberately separate from a launch deferral so callers
// can refresh displayed capacity without changing queue ownership or time.
type SessionCeilingObservation struct {
	InUse      int
	Limit      int
	ObservedAt time.Time
	Known      bool
	// ControlInUse and ControlLimit report the control lane on its own. Limit
	// stays the worker ceiling so every existing consumer keeps its meaning.
	ControlInUse int
	ControlLimit int
	// ControlConfigured reports whether a control lane exists at all, so an
	// install with no control profiles does not display a meaningless zero cap.
	ControlConfigured bool
}

// SessionCeilingCapacity is the live capacity pair the controller enforces.
type SessionCeilingCapacity struct {
	WorkerCeiling int
	// ControlCeiling bounds the control lane. Zero means no control lane, in
	// which case ControlProfileIDs is empty too and every session is a worker.
	ControlCeiling    int
	ControlProfileIDs []string
}

// sessionCeilingController is the single admission controller. Every mutation of
// the reservation set happens under its one mutex, together with the population
// read it is compared against.
type sessionCeilingController struct {
	mu              sync.Mutex
	ceiling         int
	controlCeiling  int
	controlProfiles map[string]struct{}
	reservations    map[string]*ceilingReservation
	lister          admittedSessionLister
	logger          *zap.Logger
	now             func() time.Time
	newKey          func() string
}

func newSessionCeilingController(ceiling int, lister admittedSessionLister, logger *zap.Logger) *sessionCeilingController {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &sessionCeilingController{
		ceiling:      ceiling,
		reservations: make(map[string]*ceilingReservation),
		lister:       lister,
		logger:       logger,
		now:          time.Now,
		newKey:       func() string { return "launch-" + uuid.NewString() },
	}
}

// setCapacity changes the effective capacity of both lanes without replacing
// the controller. Reservations therefore remain owned by the launch that created
// them across every Settings update, and a lane that disappears leaves its
// already-running sessions alone.
func (c *sessionCeilingController) setCapacity(capacity SessionCeilingCapacity) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	previous := c.ceiling
	previousControl := c.controlCeiling
	c.ceiling = capacity.WorkerCeiling
	c.controlCeiling = capacity.ControlCeiling
	c.controlProfiles = controlProfileSet(activeControlProfiles(capacity.ControlCeiling, capacity.ControlProfileIDs))
	c.mu.Unlock()
	return previous != capacity.WorkerCeiling || previousControl != capacity.ControlCeiling
}

// classOfLocked is the single classification rule: a session is control exactly
// when its resolved agent profile is one of the configured control profiles.
// Everything else, including an absent profile, is worker.
func (c *sessionCeilingController) classOfLocked(agentProfileID string) ceilingClass {
	if agentProfileID == "" || len(c.controlProfiles) == 0 {
		return ceilingClassWorker
	}
	if _, isControl := c.controlProfiles[agentProfileID]; isControl {
		return ceilingClassControl
	}
	return ceilingClassWorker
}

func controlProfileSet(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			set[id] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// classCeilingLocked returns the ceiling the given lane is admitted against.
// unlimitedSessionCeiling means no refusal for that lane.
func (c *sessionCeilingController) classCeilingLocked(class ceilingClass) int {
	if class == ceilingClassControl && c.controlCeiling != unlimitedSessionCeiling {
		return c.controlCeiling
	}
	return c.ceiling
}

// population returns the admitted session population across both lanes.
func (c *sessionCeilingController) population(ctx context.Context) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	counted, err := c.countedRowsLocked(ctx)
	if err != nil {
		return 0, err
	}
	return c.populationLocked(counted), nil
}

func (c *sessionCeilingController) observation(ctx context.Context) (SessionCeilingObservation, error) {
	if c == nil {
		return SessionCeilingObservation{}, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	counted, err := c.countedRowsLocked(ctx)
	observedAt := time.Now().UTC()
	if c.now != nil {
		observedAt = c.now().UTC()
	}
	observation := SessionCeilingObservation{
		Limit:             c.ceiling,
		ObservedAt:        observedAt,
		Known:             err == nil,
		ControlLimit:      c.controlCeiling,
		ControlConfigured: len(c.controlProfiles) > 0,
	}
	if err != nil {
		return observation, err
	}
	observation.InUse = c.populationLocked(counted)
	observation.ControlInUse = c.classPopulationLocked(counted, ceilingClassControl)
	return observation, nil
}

// CurrentSessionCeilingObservation exposes the controller's current bounded
// reading to composition-layer status projections. A population read failure
// returns the ceiling and a Known=false observation so queue ownership can
// still be displayed without claiming a count.
func (s *Service) CurrentSessionCeilingObservation(ctx context.Context) (SessionCeilingObservation, error) {
	if s == nil || s.sessionCeiling == nil {
		return SessionCeilingObservation{}, nil
	}
	return s.sessionCeiling.observation(ctx)
}

// SetSessionCapacity applies both lanes' effective capacity to the existing
// admission controller. A zero worker ceiling disables worker refusal while
// keeping the controller and its reservations alive for later re-enablement, and
// a zero control ceiling removes the control lane without touching sessions it
// already admitted.
func (s *Service) SetSessionCapacity(workerCeiling, controlCeiling int, controlProfileIDs []string) {
	if s == nil || s.sessionCeiling == nil {
		return
	}
	changed := s.sessionCeiling.setCapacity(SessionCeilingCapacity{
		WorkerCeiling:     workerCeiling,
		ControlCeiling:    controlCeiling,
		ControlProfileIDs: controlProfileIDs,
	})
	if !changed {
		return
	}
	if s.logger != nil {
		s.logger.Info("session ceiling capacity applied",
			zap.Int("ceiling", workerCeiling),
			zap.Int(ceilingFieldClassCeiling, controlCeiling),
			zap.Int("control_profiles", len(controlProfileIDs)),
			zap.Bool("enabled", workerCeiling != unlimitedSessionCeiling))
	}
	// A sweep both retries newly eligible work after an increase or disable and
	// refreshes the task projection after any changed observation, including a
	// decrease.
	s.signalCeilingSweep()
}

// SessionCapacity returns the current effective worker limit for the live
// admission controller. Zero means automatic worker launches are unlimited.
func (s *Service) SessionCapacity() int {
	if s == nil || s.sessionCeiling == nil {
		return unlimitedSessionCeiling
	}
	s.sessionCeiling.mu.Lock()
	defer s.sessionCeiling.mu.Unlock()
	return s.sessionCeiling.ceiling
}

// SessionControlCapacity returns the control lane's own effective limit. Zero
// means no control lane is configured.
func (s *Service) SessionControlCapacity() int {
	if s == nil || s.sessionCeiling == nil {
		return 0
	}
	s.sessionCeiling.mu.Lock()
	defer s.sessionCeiling.mu.Unlock()
	return s.sessionCeiling.controlCeiling
}

// countedRowsLocked reads the persisted half of the population, keyed by session
// id with the lane each row belongs to. It is derived at decision time rather
// than kept as a running tally, so a restart, a panic or a missed release cannot
// leak capacity permanently.
func (c *sessionCeilingController) countedRowsLocked(ctx context.Context) (map[string]ceilingClass, error) {
	if c.lister == nil {
		return map[string]ceilingClass{}, nil
	}
	refs, err := c.lister.ListAdmittedSessionRefs(ctx)
	if err != nil {
		return nil, err
	}
	counted := make(map[string]ceilingClass, len(refs))
	for _, ref := range refs {
		counted[ref.ID] = c.classOfLocked(ref.ProfileID)
	}
	return counted, nil
}

// populationLocked unions the persisted rows with the session-bound reservations by
// session id, then adds the launch-scoped reservations, each of which can collide
// with no row because no row for it exists yet. It is deliberately not the sum of
// two independent numbers, and it spans both lanes because the observation
// surface reports the instance total.
func (c *sessionCeilingController) populationLocked(counted map[string]ceilingClass) int {
	population := len(counted)
	for _, reservation := range c.reservations {
		if reservation.sessionID == "" {
			population++
			continue
		}
		if _, alreadyCounted := counted[reservation.sessionID]; !alreadyCounted {
			population++
		}
	}
	return population
}

// classPopulationLocked is populationLocked restricted to one lane. A session
// bound reservation counts toward the lane it was admitted into, so a relaunch
// or rebind cannot move occupancy between lanes.
func (c *sessionCeilingController) classPopulationLocked(counted map[string]ceilingClass, class ceilingClass) int {
	population := 0
	for _, rowClass := range counted {
		if rowClass == class {
			population++
		}
	}
	for _, reservation := range c.reservations {
		if reservation.class != class {
			continue
		}
		if reservation.sessionID == "" {
			population++
			continue
		}
		if _, alreadyCounted := counted[reservation.sessionID]; !alreadyCounted {
			population++
		}
	}
	return population
}

// reserveLocked records an in-flight reservation, keyed by session id where one
// exists and by a launch-scoped identifier otherwise.
func (c *sessionCeilingController) reserveLocked(sessionID string, class ceilingClass) string {
	key := sessionID
	if key == "" {
		key = c.newKey()
	}
	c.reservations[key] = &ceilingReservation{
		sessionID: sessionID, class: class, takenAt: c.now(),
	}
	return key
}

// admit decides one launch and, where it admits, records the reservation before
// returning. The population read and the reservation write share one critical
// section, so two launches arriving together cannot both see the same free slot.
func (c *sessionCeilingController) admit(ctx context.Context, req admissionRequest) admissionDecision {
	if c == nil {
		// A Service constructed without going through NewService (most
		// commonly a test fixture built for narrow coverage of unrelated
		// logic) has no ceiling resolved at all. Treat that the same as an
		// explicitly unlimited ceiling rather than panicking on every launch.
		return admissionDecision{admitted: true}
	}
	origin := req.origin
	if origin != launchOriginManual && origin != launchOriginAutomatic {
		origin = launchOriginAutomatic
		c.logger.Warn("launch reached the session ceiling with no origin set; classified as automatic",
			zap.String("seam", req.seam), zap.String("task_id", req.taskID), zap.String("session_id", req.sessionID))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	class := c.classOfLocked(req.agentProfileID)
	if c.classCeilingLocked(class) == unlimitedSessionCeiling {
		return c.admitUnlimitedLocked(req, origin, class)
	}

	counted, err := c.countedRowsLocked(ctx)
	if err != nil {
		return c.decideUnknownPopulationLocked(req, origin, class, err)
	}
	return c.decideLocked(ctx, req, origin, class, counted)
}

// admitUnlimitedLocked keeps launch ownership and callback accounting intact
// while avoiding a population read that cannot affect an unlimited decision.
func (c *sessionCeilingController) admitUnlimitedLocked(
	req admissionRequest, origin launchOrigin, class ceilingClass,
) admissionDecision {
	decision := admissionDecision{
		admitted: true, ceiling: unlimitedSessionCeiling, class: class,
	}
	if req.sessionID != "" {
		if _, held := c.reservations[req.sessionID]; held {
			decision.reservationKey = req.sessionID
			return decision
		}
	}
	decision.reservationKey = c.reserveLocked(req.sessionID, class)
	c.logDecision(req, origin, decision)
	return decision
}

// decideLocked is the ordinary admission decision, taken against a population the
// caller has already read inside the critical section. The instance total is
// still reported, but the refusal test is against the requesting lane's own
// ceiling: a saturated worker lane no longer refuses a control launch, and a
// saturated control lane never consumes worker capacity.
func (c *sessionCeilingController) decideLocked(
	ctx context.Context, req admissionRequest, origin launchOrigin, class ceilingClass, counted map[string]ceilingClass,
) admissionDecision {
	population := c.populationLocked(counted)
	classCeiling := c.classCeilingLocked(class)
	classPopulation := c.classPopulationLocked(counted, class)

	if decision, ok := c.alreadyAdmittedLocked(
		req, counted, population, class, classCeiling, classPopulation,
	); ok {
		return decision
	}

	decision := admissionDecision{
		population:      population,
		populationKnown: true,
		ceiling:         c.ceiling,
		class:           class,
		classCeiling:    classCeiling,
		classPopulation: classPopulation,
	}
	// A free slot in the requesting lane is offered to the queued launches
	// ahead of this request before it is consumed. That ordering is what the
	// precedence answer decides, and it is reported separately from a refusal
	// the saturated lane actually caused.
	var precedence deferredPrecedence
	yieldChecked := origin != launchOriginManual && classPopulation < classCeiling && req.yieldToDeferred != nil
	if yieldChecked {
		precedence = req.yieldToDeferred(ctx, class)
	}
	switch {
	case yieldChecked && precedence.precedes:
		decision.reasonCode = ceilingReasonDeferredPrecedes
		decision.precedesTaskID = precedence.taskID
		decision.precedesSessionID = precedence.sessionID
		decision.precedesKind = precedence.kind
		decision.precedesState = precedence.state
	case classCeiling == unlimitedSessionCeiling || classPopulation < classCeiling:
		decision.admitted = true
		decision.reservationKey = c.reserveLocked(req.sessionID, class)
	case origin == launchOriginManual:
		decision.admitted = true
		decision.manualOverride = true
		decision.reasonCode = ceilingReasonManualOverride
		decision.reservationKey = c.reserveLocked(req.sessionID, class)
	default:
		decision.reasonCode = refusedReasonFor(class)
	}
	c.logDecision(req, origin, decision)
	return decision
}

// refusedReasonFor keeps the worker lane's historical reason code byte-identical
// and gives the control lane its own, so an operator reading a card or a log
// line can tell which lane refused without inferring it from counts.
func refusedReasonFor(class ceilingClass) string {
	if class == ceilingClassControl {
		return ceilingReasonControlRefused
	}
	return ceilingReasonRefused
}

// alreadyAdmittedLocked answers a request for a session that already holds a
// reservation or is already counted, so one launch consumes at most one unit of
// capacity however many seams it passes through.
func (c *sessionCeilingController) alreadyAdmittedLocked(
	req admissionRequest,
	counted map[string]ceilingClass,
	population int,
	class ceilingClass,
	classCeiling int,
	classPopulation int,
) (admissionDecision, bool) {
	if req.sessionID == "" {
		return admissionDecision{}, false
	}
	decision := admissionDecision{
		admitted:        true,
		population:      population,
		populationKnown: true,
		ceiling:         c.ceiling,
		class:           class,
		classCeiling:    classCeiling,
		classPopulation: classPopulation,
	}
	if _, held := c.reservations[req.sessionID]; held {
		decision.reservationKey = req.sessionID
		return decision, true
	}
	if _, isCounted := counted[req.sessionID]; isCounted {
		return decision, true
	}
	return admissionDecision{}, false
}

// decideUnknownPopulationLocked fails closed for automatic launches and open for
// manual ones. Reservations already held are untouched: the failure concerns the
// counted rows only.
func (c *sessionCeilingController) decideUnknownPopulationLocked(
	req admissionRequest, origin launchOrigin, class ceilingClass, err error,
) admissionDecision {
	c.logger.Error("session ceiling could not read the admitted session population",
		zap.String("seam", req.seam), zap.String("task_id", req.taskID),
		zap.String("session_id", req.sessionID), zap.String("origin", string(origin)), zap.Error(err))

	decision := admissionDecision{
		ceiling:      c.ceiling,
		class:        class,
		classCeiling: c.classCeilingLocked(class),
		reasonCode:   ceilingReasonUnknownPopulation,
	}
	if origin == launchOriginManual {
		decision.admitted = true
		decision.manualOverride = true
		decision.reservationKey = c.reserveLocked(req.sessionID, class)
	}
	c.logDecision(req, origin, decision)
	return decision
}

// logDecision emits the admission log line, which carries every reason code and is
// the only universal observable of a decision.
func (c *sessionCeilingController) logDecision(req admissionRequest, origin launchOrigin, decision admissionDecision) {
	fields := []zap.Field{
		zap.String("seam", req.seam),
		zap.String("task_id", req.taskID),
		zap.String("session_id", req.sessionID),
		zap.String("origin", string(origin)),
		zap.Int(ceilingFieldCeiling, decision.ceiling),
		zap.Bool("admitted", decision.admitted),
		zap.String(ceilingFieldClass, string(decision.class)),
		zap.Int(ceilingFieldClassCeiling, decision.classCeiling),
	}
	if decision.populationKnown {
		fields = append(fields, zap.Int("population", decision.population))
		fields = append(fields, zap.Int(ceilingFieldClassPopulation, decision.classPopulation))
	}
	if decision.reasonCode != "" {
		fields = append(fields, zap.String(ceilingFieldReasonCode, decision.reasonCode))
	}
	if decision.precedesTaskID != "" {
		fields = append(fields,
			zap.String(ceilingFieldPrecedesTask, decision.precedesTaskID),
			zap.String(ceilingFieldPrecedesSession, decision.precedesSessionID),
			zap.String(ceilingFieldPrecedesKind, string(decision.precedesKind)),
			zap.String(ceilingFieldPrecedesState, decision.precedesState))
	}
	c.logger.Info("session ceiling admission decision", fields...)
}

// release drops a reservation by its key. Releasing an unknown or already-released
// key succeeds and decrements nothing.
func (c *sessionCeilingController) release(key string) {
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.reservations, key)
}

// rebind moves a launch-scoped reservation onto the session id that launch just
// created, as one operation rather than a release followed by an acquire, so the
// population never momentarily drops and no concurrent admission can take the
// freed unit. Where a reservation already occupies that session id — two
// launches racing to the same session, such as an Office identity-owned
// session two concurrent starts converge onto via
// EnsureSessionForAgentWithCreation — the launch-scoped reservation is
// released instead of overwriting the winner's slot: this is still one
// launch, mirroring rekey's own collision handling. Returning false leaves the
// caller's key pointing at the now-deleted launch-scoped entry, so its own
// later release is a harmless no-op rather than deleting the winner's still
// in-flight reservation.
func (c *sessionCeilingController) rebind(launchKey, sessionID string) bool {
	if launchKey == "" || sessionID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	reservation, held := c.reservations[launchKey]
	if !held {
		return false
	}
	delete(c.reservations, launchKey)
	if _, collision := c.reservations[sessionID]; collision {
		return false
	}
	reservation.sessionID = sessionID
	c.reservations[sessionID] = reservation
	return true
}

// rekey moves a reservation from the session that was gated onto the replacement
// session that will actually be launched. Where the replacement is already counted
// or already reserved, the original is released and no second unit is consumed:
// this is still one launch.
func (c *sessionCeilingController) rekey(ctx context.Context, fromSessionID, toSessionID string) bool {
	if fromSessionID == "" || toSessionID == "" || fromSessionID == toSessionID {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	reservation, held := c.reservations[fromSessionID]
	if !held {
		return false
	}
	delete(c.reservations, fromSessionID)

	if _, alreadyReserved := c.reservations[toSessionID]; alreadyReserved {
		// Do not transfer ownership onto a reservation the caller does not
		// hold: the source key is already gone (deleted above), so the
		// caller's later releaseIfNotConsumed becomes a no-op instead of
		// deleting the actual holder's reservation and undercounting the
		// population, matching rebind's collision behavior.
		return false
	}
	if counted, err := c.countedRowsLocked(context.WithoutCancel(ctx)); err == nil {
		if _, alreadyCounted := counted[toSessionID]; alreadyCounted {
			return true
		}
	}
	reservation.sessionID = toSessionID
	c.reservations[toSessionID] = reservation
	return true
}

// handOffOrAdmit serves the dynamic-route relaunch seam. Where the session is still
// in the population, its counted membership is converted into a reservation under
// the same mutex: a relaunch replaces one agent process with another rather than
// adding one, so the hand-off is never refused and never changes the count.
// Where the session is not counted there is no slot to hand off and this is an
// ordinary admission request.
func (c *sessionCeilingController) handOffOrAdmit(ctx context.Context, req admissionRequest) admissionDecision {
	if c == nil {
		return admissionDecision{admitted: true}
	}
	origin := req.origin
	if origin != launchOriginManual && origin != launchOriginAutomatic {
		origin = launchOriginAutomatic
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	class := c.classOfLocked(req.agentProfileID)
	if c.classCeilingLocked(class) == unlimitedSessionCeiling {
		return c.admitUnlimitedLocked(req, origin, class)
	}

	counted, err := c.countedRowsLocked(ctx)
	if err != nil {
		return c.decideUnknownPopulationLocked(req, origin, class, err)
	}
	rowClass, isCounted := counted[req.sessionID]
	if req.sessionID == "" || !isCounted {
		return c.decideLocked(ctx, req, origin, class, counted)
	}
	// A relaunch keeps the lane its counted row already occupies. Re-deriving the
	// class from the caller's profile here could move a running control session
	// into the worker lane and consume worker capacity for the same process.
	class = rowClass

	decision := admissionDecision{
		admitted:        true,
		handedOff:       true,
		reservationKey:  req.sessionID,
		population:      c.populationLocked(counted),
		populationKnown: true,
		ceiling:         c.ceiling,
		class:           class,
		classCeiling:    c.classCeilingLocked(class),
		classPopulation: c.classPopulationLocked(counted, class),
	}
	c.reservations[req.sessionID] = &ceilingReservation{
		sessionID: req.sessionID, class: class, takenAt: c.now(),
	}
	c.logDecision(req, origin, decision)
	return decision
}

// isSessionCeilingBacked implements AC-41: whether a session_id is currently
// backed by an in-flight reservation or a counted AC-1 population row. A nil
// controller (a Service built without one resolved, e.g. narrow test
// fixtures) reports every session as backed, the same "no ceiling in effect"
// posture admit/handOffOrAdmit already take. This method is consulted only
// by Executor's observation-only bypass detector (AC-41a) and never gates,
// delays or fails a launch itself.
func (c *sessionCeilingController) isSessionCeilingBacked(ctx context.Context, sessionID string) (bool, error) {
	if c == nil || sessionID == "" {
		return true, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, reserved := c.reservations[sessionID]; reserved {
		return true, nil
	}
	counted, err := c.countedRowsLocked(ctx)
	if err != nil {
		return false, err
	}
	_, ok := counted[sessionID]
	return ok, nil
}

// expireStaleReservations releases reservations whose launch neither reached a
// counted state nor reported failure inside the launch budget. It is the backstop,
// not the primary release edge.
func (c *sessionCeilingController) expireStaleReservations() int {
	budget := constants.AgentLaunchTimeout + reservationExpiryAllowance
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()
	released := 0
	for key, reservation := range c.reservations {
		if now.Sub(reservation.takenAt) <= budget {
			continue
		}
		delete(c.reservations, key)
		released++
		c.logger.Warn("session ceiling released a reservation whose launch never reached a counted state",
			zap.String("reservation_key", key),
			zap.String("session_id", reservation.sessionID),
			zap.Duration("held_for", now.Sub(reservation.takenAt)),
			zap.Duration("budget", budget))
	}
	return released
}
