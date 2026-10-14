package runtime

import (
	"context"
	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

func (r *ProfileExecutionResolver) WithTurnAdmission(ctx context.Context, current dynamic.RouteDecision, claim func() error) error {
	if r == nil || r.engine == nil || current.Generation <= 0 {
		return claim()
	}
	profile, err := r.loadDynamicProfileForSession(ctx, current.LogicalProfileID, current.SessionID, "")
	if err != nil {
		return err
	}
	return r.engine.WithTurnAdmission(ctx, profile, current, claim)
}
