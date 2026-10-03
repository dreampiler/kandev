package dynamic

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// LimitObserver is told when a candidate hits a provider usage or rate limit,
// so the usage recorded up to that moment can be kept as evidence of where the
// account's real limit lies. It observes only; it never changes routing.
type LimitObserver interface {
	ObserveLimit(ctx context.Context, executionProfileID string, code routingerr.Code, at time.Time)
}

// WithLimitObserver records limit hits for usage analysis.
func WithLimitObserver(observer LimitObserver) EngineOption {
	return func(engine *Engine) { engine.limits = observer }
}

func (e *Engine) observeLimit(ctx context.Context, candidateID string, failure *routingerr.Error) {
	if e.limits == nil || failure == nil || candidateID == "" {
		return
	}
	switch failure.Code {
	case routingerr.CodeQuotaLimited, routingerr.CodeRateLimited:
		e.limits.ObserveLimit(ctx, candidateID, failure.Code, e.now())
	}
}
