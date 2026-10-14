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

func (e *Engine) admissionDeadline(c Candidate, now time.Time) (time.Time, bool) {
	checker, ok := e.usage.(AdmissionChecker)
	if !ok {
		return time.Time{}, false
	}
	score, err := checker.CheckAdmission(context.Background(), c, "", now)
	return score.AdmissionRetryAt, err == nil && score.AdmissionReason != "" && score.AdmissionRetryAt.After(now)
}
