package orchestrator

import (
	"errors"

	"github.com/kandev/kandev/internal/orchestrator/executor"
)

// ceilingReplayResult pairs a replay's disposition with the failure that
// produced it. err and detail are zero for every non-failure outcome, so
// settleCeilingReplay can log why a non-ceiling failure happened instead of
// only that one did.
type ceilingReplayResult struct {
	outcome ceilingReplayOutcome
	err     error
	detail  string
}

func ceilingReplaySucceededResult() ceilingReplayResult {
	return ceilingReplayResult{outcome: ceilingReplaySucceeded}
}

func ceilingReplayDeferredResult() ceilingReplayResult {
	return ceilingReplayResult{outcome: ceilingReplayStillDeferred}
}

func ceilingReplaySupersededResult() ceilingReplayResult {
	return ceilingReplayResult{outcome: ceilingReplaySuperseded}
}

func ceilingReplayRunClosedResult() ceilingReplayResult {
	return ceilingReplayResult{outcome: ceilingReplayRunClosed}
}

func ceilingReplayFailedResult(err error, detail string) ceilingReplayResult {
	return ceilingReplayResult{outcome: ceilingReplayFailed, err: err, detail: detail}
}

// ceilingReplayResultFromExecution is the shared classification for the entry
// points (start, start_created, resume — and, by the same contract, any future
// kind) that return (*executor.TaskExecution, error): a non-nil execution is
// success, a nil execution with a nil error is still-deferred (the seam's own
// gate re-persisted the record internally), and anything else is a non-ceiling
// failure whose error is retained for diagnostics.
func ceilingReplayResultFromExecution(execution *executor.TaskExecution, err error) ceilingReplayResult {
	switch {
	case errors.Is(err, ErrCeilingLaunchSuperseded):
		return ceilingReplaySupersededResult()
	case err != nil:
		return ceilingReplayFailedResult(err, "dispatch failed")
	case execution == nil:
		return ceilingReplayDeferredResult()
	default:
		return ceilingReplaySucceededResult()
	}
}
