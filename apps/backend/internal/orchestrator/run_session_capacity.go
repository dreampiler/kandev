package orchestrator

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

// RunSessionCapacitySource supplies Office-owned population and restores only
// its capacity-deferred runs. A zero available value means capacity is unlimited.
type RunSessionCapacitySource interface {
	ListAdmittedRunSessionIDs(context.Context) ([]string, error)
	ResumeSessionCapacityRuns(context.Context, int) error
}

// SetRunSessionCapacitySource is wired before automatic launch consumers start.
func (s *Service) SetRunSessionCapacitySource(source RunSessionCapacitySource) {
	if s.sessionCeiling == nil {
		return
	}
	s.sessionCeiling.mu.Lock()
	s.sessionCeiling.runSessions = source
	s.sessionCeiling.mu.Unlock()
}

func runSessionCapacityKey(id string) string { return "office-run:" + id }

// RunSessionCapacityReservation owns one launch's reservation, independently of
// its run's claim and of every predecessor or successor attempt.
type RunSessionCapacityReservation struct {
	service *Service
	key     string
	once    sync.Once
}

// Confirm transfers counting to the persisted preparing/running run-session row.
func (r *RunSessionCapacityReservation) Confirm() {
	if r != nil {
		r.once.Do(func() { r.service.sessionCeiling.release(r.key) })
	}
}

// Release returns a failed launch's reservation and requests the existing sweep.
func (r *RunSessionCapacityReservation) Release() {
	if r != nil {
		r.once.Do(func() { r.service.releaseCeilingReservation(r.key) })
	}
}

// ReserveRunSessionCapacity shares the task admission mutex and population.
// Office launches are automatic; transport and provider tier grant no bypass.
func (s *Service) ReserveRunSessionCapacity(ctx context.Context, sessionID string) (*RunSessionCapacityReservation, bool) {
	if s == nil || s.sessionCeiling == nil {
		return nil, true
	}
	decision := s.sessionCeiling.admit(ctx, admissionRequest{
		sessionID: runSessionCapacityKey(sessionID),
		origin:    launchOriginAutomatic, seam: "officeRunSession",
	})
	if !decision.admitted {
		return nil, false
	}
	return &RunSessionCapacityReservation{service: s, key: decision.reservationKey}, true
}

// NotifyRunSessionCapacityReleased is called after a run-session terminal CAS.
func (s *Service) NotifyRunSessionCapacityReleased() { s.signalCeilingSweep() }

func (s *Service) retryRunSessionCapacity(ctx context.Context) {
	c := s.sessionCeiling
	if c == nil {
		return
	}
	c.mu.Lock()
	source := c.runSessions
	available := 0
	if source != nil && c.ceiling != unlimitedSessionCeiling {
		counted, err := c.countedRowsLocked(ctx)
		if err != nil {
			c.mu.Unlock()
			return
		}
		available = c.ceiling - c.populationLocked(counted)
		if available <= 0 {
			c.mu.Unlock()
			return
		}
	}
	c.mu.Unlock()
	if source != nil {
		if err := source.ResumeSessionCapacityRuns(ctx, available); err != nil {
			s.logger.Warn("could not resume capacity-deferred Office runs", zap.Error(err))
		}
	}
}
