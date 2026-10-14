package dynamic

import (
	"context"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"time"
)

// AdmissionChecker rechecks mutable account capacity at the serialized claim.
type AdmissionChecker interface {
	CheckAdmission(context.Context, Candidate, string, time.Time) (PaceScore, error)
}

type LimitFailureResolver interface {
	ResolveLimitFailure(context.Context, Candidate, *routingerr.Error) *routingerr.Error
}

type ClassifiedLimitObserver interface {
	ObserveClassifiedLimit(context.Context, Candidate, *routingerr.Error, time.Time)
}

func (e *Engine) observeClassifiedLimit(ctx context.Context, c Candidate, failure *routingerr.Error) {
	if failure.Code != routingerr.CodeRateLimited && failure.Code != routingerr.CodeQuotaLimited {
		return
	}
	if observer, ok := e.limits.(ClassifiedLimitObserver); ok {
		observer.ObserveClassifiedLimit(ctx, c, failure, e.now())
		return
	}
	e.observeLimit(ctx, c.ID, failure)
}

// claimInputs reads capacity while the admission lock is held and the state
// lock is free. The same immutable scores gate selection and resource waiting.
func (e *Engine) claimInputs(ctx context.Context, profile Profile, now time.Time) RankOptions {
	inputs := e.rankInputs(ctx, profile)
	checker, ok := e.usage.(AdmissionChecker)
	if !ok {
		return inputs
	}
	scores := make(map[string]PaceScore, len(profile.Candidates))
	for id, score := range inputs.Scores {
		scores[id] = score
	}
	for _, candidate := range profile.Candidates {
		if !candidate.Enabled {
			continue
		}
		admission, err := checker.CheckAdmission(ctx, candidate, profile.AdmissionSessionID, now)
		if err != nil {
			continue
		}
		score := scores[candidate.ID]
		score.AdmissionReason, score.AdmissionRetryAt = admission.AdmissionReason, admission.AdmissionRetryAt
		scores[candidate.ID] = score
	}
	inputs.Scores = scores
	return inputs
}
