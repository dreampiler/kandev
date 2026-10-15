package backendapp

import (
	"context"
	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"time"
)

func (s *dynamicUsageSnapshot) CheckAdmission(ctx context.Context, c dynamicruntime.Candidate, sessionID string, now time.Time) (dynamicruntime.PaceScore, error) {
	if c.RemoteExecution || s.usage == nil || s.usage.tierLimits == nil {
		return dynamicruntime.PaceScore{}, nil
	}
	verdict, err := s.usage.tierLimits.Check(ctx, c.ID, sessionID, now)
	return dynamicruntime.PaceScore{AdmissionReason: verdict.Reason, AdmissionRetryAt: verdict.RetryAt}, err
}

func (s *dynamicUsageSnapshot) ResolveLimitFailure(ctx context.Context, c dynamicruntime.Candidate, failure *routingerr.Error) *routingerr.Error {
	if s.usage == nil || s.usage.tierLimits == nil {
		return failure
	}
	return s.usage.tierLimits.resolveFailure(ctx, c, failure)
}
