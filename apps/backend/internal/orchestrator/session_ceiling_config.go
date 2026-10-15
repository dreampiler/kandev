package orchestrator

import (
	"github.com/kandev/kandev/internal/system/hostmemory"
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
//
// guard is the host-memory pressure threshold pair. When it is enabled the
// controller asks the process's host-memory reader on each admission; a
// disabled guard leaves memoryReader nil and never reads the host.
func newSessionCeilingForRepo(
	repo interface{}, capacity SessionCeilingCapacity, guard hostmemory.Gate, logger *zap.Logger,
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
	controller.controlProfileIDs = activeControlIDs(capacity.ControlCeiling, capacity.ControlProfileIDs)
	controller.controlWorkflowIDs = activeControlIDs(capacity.ControlCeiling, capacity.ControlWorkflowIDs)
	if guard.Enabled {
		controller.memoryGate = guard
		controller.memoryReader = hostmemory.NewReader()
	}
	return controller
}

// activeControlIDs is the single enforcement boundary for "the control lane
// exists". Classifying a session as control removes it from the worker
// population, so a control id list without a control ceiling would let those
// sessions bypass worker capacity while still being refused by it. Both halves
// are therefore required here, before any classification reads the set: with no
// control ceiling the set is dropped and every session is a worker, exactly as
// before the lane existed. It is shared by the profile set and the workflow set,
// and drops blank and duplicate ids so one identity cannot occupy two slots.
func activeControlIDs(controlCeiling int, ids []string) []string {
	if controlCeiling <= unlimitedSessionCeiling || len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	active := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		active = append(active, id)
	}
	if len(active) == 0 {
		return nil
	}
	return active
}
