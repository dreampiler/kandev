package orchestrator

import (
	"context"
	"github.com/kandev/kandev/internal/system/sessioncapacity"
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
func (s *Service) ReserveRunSessionCapacity(ctx context.Context, sessionID string, grades ...string) (*RunSessionCapacityReservation, bool) {
	if s == nil || s.sessionCeiling == nil {
		return nil, true
	}
	grade := ""
	if len(grades) > 0 {
		grade = grades[0]
	}
	decision := s.sessionCeiling.admit(ctx, admissionRequest{
		grade:     grade,
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
	if source == nil {
		c.mu.Unlock()
		return
	}
	available, gradeAvailable, known := c.runCapacityAvailableLocked(ctx)
	c.mu.Unlock()
	if known {
		if graded, ok := source.(interface {
			ResumeExecutionCapacityRuns(context.Context, int, map[string]int) error
		}); ok && len(gradeAvailable) > 0 {
			if err := graded.ResumeExecutionCapacityRuns(ctx, available, gradeAvailable); err != nil {
				s.logger.Warn("could not resume grade-deferred Office runs", zap.Error(err))
			}
			return
		}
		if err := source.ResumeSessionCapacityRuns(ctx, available); err != nil {
			s.logger.Warn("could not resume capacity-deferred Office runs", zap.Error(err))
		}
	}
}

func (c *sessionCeilingController) runCapacityAvailableLocked(ctx context.Context) (int, map[string]int, bool) {
	gradeAvailable := map[string]int{}
	if c.ceiling == unlimitedSessionCeiling && len(c.gradeLimits) == 0 {
		return 0, gradeAvailable, true
	}
	counted, err := c.countedRowsLocked(ctx)
	if err != nil {
		return 0, nil, false
	}
	available := 0
	if c.ceiling != unlimitedSessionCeiling {
		available = c.ceiling - c.populationLocked(counted)
		if available <= 0 {
			return 0, nil, false
		}
	}
	if c.policy == sessioncapacity.PolicyAll {
		for grade, maximum := range c.gradeLimits {
			gradeAvailable[grade] = max(0, maximum-c.gradePopulationLocked(counted, grade))
		}
	}
	return available, gradeAvailable, true
}
