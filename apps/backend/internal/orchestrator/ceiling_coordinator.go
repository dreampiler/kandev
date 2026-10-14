package orchestrator

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// coordinatorAllowanceAge is how long a workspace coordinator's own queued
// launch must have waited before a saturated worker lane admits it anyway
// (REQ-AGENTS-SESSION-CEILING-005). It matches the five-minute retry bound of
// AC-AGENTS-SESSION-CEILING-001.12, so a lane that drains on its own keeps the
// ordinary free-slot path and only a lane that stays full is overshot.
const coordinatorAllowanceAge = 5 * time.Minute

// CeilingCoordinatorProfiles answers whether an agent profile is a workspace
// coordinator, from the durable profile row alone. It is wired once at the
// composition root; a launch caller can never supply the answer.
type CeilingCoordinatorProfiles interface {
	IsWorkspaceCoordinatorProfile(ctx context.Context, profileID string) (bool, error)
}

// SetCeilingCoordinatorProfiles wires the coordinator reader. Nil leaves every
// launch an ordinary one.
func (s *Service) SetCeilingCoordinatorProfiles(r CeilingCoordinatorProfiles) {
	s.ceilingCoordinators = r
}

// isCeilingCoordinatorProfile fails closed: an empty profile, an unwired
// reader, or a read error is never a coordinator.
func (s *Service) isCeilingCoordinatorProfile(ctx context.Context, profileID string) bool {
	if s.ceilingCoordinators == nil || profileID == "" {
		return false
	}
	ok, err := s.ceilingCoordinators.IsWorkspaceCoordinatorProfile(ctx, profileID)
	if err != nil {
		s.logger.Zap().Debug("could not resolve coordinator profile for ceiling admission",
			zap.String("agent_profile_id", profileID), zap.Error(err))
		return false
	}
	return ok
}

// withCoordinatorAdmission marks an automatic coordinator request and records
// when its own queued launch was first refused. It runs before the controller
// mutex is taken, so the profile and task reads never hold capacity.
func (s *Service) withCoordinatorAdmission(ctx context.Context, req admissionRequest) admissionRequest {
	if req.origin == launchOriginManual || !s.isCeilingCoordinatorProfile(ctx, req.agentProfileID) {
		return req
	}
	req.coordinator = true
	if req.taskID == "" || s.repo == nil {
		return req
	}
	task, err := s.repo.GetTask(ctx, req.taskID)
	if err != nil || task == nil {
		return req
	}
	if deferral, readable := s.readCeilingDeferralCandidate(task); readable &&
		s.ceilingRequestOwnsDeferredRecord(ctx, req, task.ID, deferral) {
		req.coordinatorQueuedSince = deferral.QueuedAt
	}
	return req
}

// ceilingCoordinatorTasks reports which listed deferred records belong to a
// workspace coordinator. Only Office tasks can carry a coordinator launch, and
// each distinct profile is resolved once per call.
func (s *Service) ceilingCoordinatorTasks(ctx context.Context, tasks []*models.Task) map[string]bool {
	if s.ceilingCoordinators == nil {
		return nil
	}
	flags := make(map[string]bool)
	byProfile := make(map[string]bool)
	for _, task := range tasks {
		if task == nil || !task.IsFromOffice {
			continue
		}
		deferral, readable := s.readCeilingDeferralCandidate(task)
		if !readable {
			continue
		}
		profileID := s.ceilingDeferredProfile(ctx, deferral)
		coordinator, seen := byProfile[profileID]
		if !seen {
			coordinator = s.isCeilingCoordinatorProfile(ctx, profileID)
			byProfile[profileID] = coordinator
		}
		if coordinator {
			flags[task.ID] = true
		}
	}
	return flags
}

// sortCeilingReplayQueue orders deferred records the way both the sweep and
// the precedence scan consume them: coordinator records first, then the
// AC-AGENTS-SESSION-CEILING-001.11 order within each group.
func (s *Service) sortCeilingReplayQueue(ctx context.Context, tasks []*models.Task) map[string]bool {
	coordinators := s.ceilingCoordinatorTasks(ctx, tasks)
	sort.SliceStable(tasks, func(i, j int) bool {
		return ceilingReplayOrderLess(coordinators, tasks[i], tasks[j])
	})
	return coordinators
}

// launchOwnerClosedSignal is implemented by a credential re-mint error that
// reports the run a queued launch serves no longer waits for it: the run is
// terminal or already bound to a session. The replay drops such a record
// instead of starting an agent for work that is over.
type launchOwnerClosedSignal interface {
	LaunchOwnerClosed() bool
}

// ceilingDetailLaunchOwnerClosed is the drop detail for that record.
const ceilingDetailLaunchOwnerClosed = "the run this launch serves is no longer waiting for it"

func isLaunchOwnerClosed(err error) bool {
	var signal launchOwnerClosedSignal
	return errors.As(err, &signal) && signal.LaunchOwnerClosed()
}
