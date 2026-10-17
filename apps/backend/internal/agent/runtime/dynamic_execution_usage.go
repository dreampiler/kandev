package runtime

import (
	"context"
	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/agent/usage"
	"time"
)

// CandidateExecutionUsageReader must use the original session's actual actor,
// never the backend account or another profile's representative host cache.
type CandidateExecutionUsageReader func(context.Context, string, *models.AgentProfile) *usage.ExecutionUsageReport
type NativeCandidateExecutionUsageReader func(context.Context, string, string, *models.AgentProfile) *usage.ExecutionUsageReport

func (r *ProfileExecutionResolver) SetNativeCandidateExecutionUsageReader(reader NativeCandidateExecutionUsageReader) {
	r.nativeCandidateUsageReader = reader
}

// Installed before service start. Nil preserves the disabled legacy path.
func (r *ProfileExecutionResolver) SetCandidateExecutionUsageReader(reader CandidateExecutionUsageReader) {
	r.candidateUsageReader = reader
}

func (r *ProfileExecutionResolver) LoadDynamicProfileForSession(ctx context.Context, profileID, sessionID string) (dynamic.Profile, error) {
	return r.loadSelectionProfile(ctx, profileID, sessionID)
}

func (r *ProfileExecutionResolver) loadSelectionProfile(ctx context.Context, profileID, sessionID string) (dynamic.Profile, error) {
	profile, err := r.loadDynamicProfile(ctx, profileID)
	if err != nil || !r.engine.OrderedTiersEnabled() || sessionID == "" {
		return profile, err
	}
	// This runs before Engine takes its selection mutex. The production reader
	// only peeks existing observations and cannot create an actor or provider call.
	for index := range profile.Candidates {
		candidate := &profile.Candidates[index]
		if !candidate.Enabled {
			continue
		}
		concrete, readErr := r.profiles.GetAgentProfile(ctx, candidate.ID)
		if readErr != nil || concrete == nil {
			continue
		}
		report, scoped := usage.ReadCandidateUsage(ctx, concrete.ID, concrete.Model)
		if !scoped {
			if nativeInferenceRoute(sessionID) && r.nativeCandidateUsageReader != nil {
				report = r.nativeCandidateUsageReader(ctx, sessionID, profileID, concrete)
			} else if r.candidateUsageReader != nil {
				report = r.candidateUsageReader(ctx, sessionID, concrete)
			}
		}
		model := concrete.Model
		// A request-scoped producer has composed the real launch model. Persisted
		// session overrides can legitimately differ from the candidate default.
		if scoped && report != nil && report.Model != "" {
			model = report.Model
		}
		candidate.UsagePolicy = usage.SelectionPolicyForReport(concrete.ID, model, report)
		candidate.Usage = dynamic.CandidateUsageFromReport(report, model, time.Now())
	}
	return profile, nil
}
