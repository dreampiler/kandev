package orchestrator

import (
	"go.uber.org/zap"
)

// unlimitedSessionCeiling is the configured value that disables refusal, matching
// the wip_limit convention.
const unlimitedSessionCeiling = 0

// newSessionCeilingForRepo builds the controller the service owns, binding the
// repository as the persisted half of its population source.
//
// The repository is reached through a narrow consumer-side interface and a type
// assertion, following the idiom the other repository consumers in this package
// use. That idiom degrades silently, and here the degraded state is dangerous
// rather than merely reduced: an unbound controller still answers, but it counts
// only its own in-process reservations, so every session already running on the
// instance is invisible to it and the ceiling admits without bound. A build-time
// assertion covers the production repository; this warning covers everything else.
func newSessionCeilingForRepo(
	repo interface{}, capacity SessionCeilingCapacity, logger *zap.Logger,
) *sessionCeilingController {
	if logger == nil {
		logger = zap.NewNop()
	}
	lister, ok := repo.(admittedSessionLister)
	if !ok {
		logger.Warn("repository cannot enumerate admitted sessions; the session ceiling will count only in-process reservations")
		lister = nil
	}
	controller := newSessionCeilingController(capacity.WorkerCeiling, lister, logger)
	controller.controlCeiling = capacity.ControlCeiling
	controller.controlProfiles = controlProfileSet(activeControlProfiles(capacity.ControlCeiling, capacity.ControlProfileIDs))
	return controller
}

// activeControlProfiles is the single enforcement boundary for "the control lane
// exists". Classifying a session as control removes it from the worker
// population, so a profile list without a control ceiling would let those sessions
// bypass worker capacity while still being refused by it. Both halves are
// therefore required here, before any classification reads the set: with no
// control ceiling the set is dropped and every session is a worker, exactly as
// before the lane existed.
func activeControlProfiles(controlCeiling int, ids []string) []string {
	if controlCeiling <= unlimitedSessionCeiling {
		return nil
	}
	return ids
}
