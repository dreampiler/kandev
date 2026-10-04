package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// ProviderLimitReader supplies the operator-entered provider limit settings.
type ProviderLimitReader interface {
	ListProviderLimits(ctx context.Context) ([]dynamic.ProviderLimit, error)
}

// SetProviderLimitReader wires the operator-entered provider blocks into
// candidate resolution.
func (r *ProfileExecutionResolver) SetProviderLimitReader(reader ProviderLimitReader) {
	r.providerLimits = reader
}

// SetResourceWaitObserver forwards the engine's resource-wait notification.
func (r *ProfileExecutionResolver) SetResourceWaitObserver(observer func(sessionID string, generation int64, deadline time.Time)) {
	if r == nil || r.engine == nil {
		return
	}
	r.engine.SetResourceWaitObserver(observer)
}

// providerLimitMap loads the provider settings keyed by provider. A read
// failure answers no settings: an operator block is an addition to failure
// suspensions, so losing it never makes a suspended candidate selectable.
func (r *ProfileExecutionResolver) providerLimitMap(ctx context.Context) map[string]dynamic.ProviderLimit {
	if r.providerLimits == nil {
		return nil
	}
	limits, err := r.providerLimits.ListProviderLimits(ctx)
	if err != nil {
		return nil
	}
	byProvider := make(map[string]dynamic.ProviderLimit, len(limits))
	for _, limit := range limits {
		byProvider[dynamic.NormalizeProvider(limit.Provider)] = limit
	}
	return byProvider
}

// applyResourceLimits gives each enabled candidate its model circuit key, when
// its provider meters models separately, and the operator block covering its
// provider.
func (r *ProfileExecutionResolver) applyResourceLimits(ctx context.Context, candidates []dynamic.Candidate) {
	limits := r.providerLimitMap(ctx)
	now := time.Now()
	for index := range candidates {
		candidate := &candidates[index]
		if !candidate.Enabled || candidate.ModelID == "" {
			continue
		}
		if dynamic.ModelScopedCandidate(*candidate) {
			candidate.ModelKey = dynamic.ResourceKey(dynamic.ScopeModel, candidate.BindingKey+"|"+candidate.ModelID)
		}
		if limit, ok := limits[dynamic.ProviderOf(candidate.ModelID)]; ok {
			if until, blocked := limit.BlockedUntilCandidate(*candidate, now); blocked {
				candidate.SuspendedUntil = until
			}
		}
	}
}

// declaredFreeModel reports whether a dynamic route stores this concrete
// profile as a free model. The route row is the reverse direction of a profile
// load: a failure, a preview and a status query all name the concrete profile,
// while the configured cost class lives on the route row of the dynamic profile
// that owns it.
//
// The class is a statement about the model rather than about one row, so any
// referencing route that declares the model free answers free. A profile no
// dynamic profile references, an unreadable route document, or a non-free class
// leaves the answer false, which keeps the model ID's own classification in
// force.
func (r *ProfileExecutionResolver) declaredFreeModel(ctx context.Context, executionProfileID string) bool {
	if r.dynamic == nil || executionProfileID == "" {
		return false
	}
	references, err := r.dynamic.ListDynamicProfileReferencesByExecutionProfile(ctx, executionProfileID)
	if err != nil {
		return false
	}
	for _, reference := range references {
		_, routes, err := r.dynamic.GetDynamicAgentProfile(ctx, reference.ProfileID)
		if err != nil {
			continue
		}
		for _, route := range routes {
			if route.ExecutionProfileID != executionProfileID {
				continue
			}
			if routeCostClass(route.RulesJSON) == dynamic.CostFree {
				return true
			}
		}
	}
	return false
}

// resourceCandidate builds the resource identity of one concrete profile, the
// same keys a selection would use for it.
func (r *ProfileExecutionResolver) resourceCandidate(ctx context.Context, executionProfileID string) (dynamic.Candidate, bool) {
	if r == nil || r.engine == nil || r.profiles == nil || executionProfileID == "" {
		return dynamic.Candidate{}, false
	}
	profile, err := r.profiles.GetAgentProfile(ctx, executionProfileID)
	if err != nil || profile == nil || profile.DeletedAt != nil {
		return dynamic.Candidate{}, false
	}
	candidate := dynamic.Candidate{
		ID: executionProfileID, Enabled: true,
		ModelID:    strings.TrimSpace(profile.Model),
		BindingKey: dynamic.ResourceKey(dynamic.ScopeProfile, executionProfileID),
	}
	if r.declaredFreeModel(ctx, executionProfileID) {
		candidate.Selection.Model.Cost = dynamic.CostFree
	}
	if r.bindingResolver != nil {
		candidate.BindingKey = dynamic.ResourceKey(
			dynamic.ScopeCredential,
			r.bindingResolver.Resolve(profileCredentialBindingDescriptor(profile), executionProfileID),
		)
	}
	candidates := []dynamic.Candidate{candidate}
	r.applyResourceLimits(ctx, candidates)
	return candidates[0], true
}

// CandidateSuspension reports a concrete profile's resource health. A profile
// that cannot be resolved reports an unknown verdict instead of a healthy one.
func (r *ProfileExecutionResolver) CandidateSuspension(
	ctx context.Context,
	executionProfileID string,
	now time.Time,
) (dynamic.CandidateSuspension, bool) {
	candidate, ok := r.resourceCandidate(ctx, executionProfileID)
	if !ok {
		return dynamic.CandidateSuspension{}, false
	}
	return r.engine.SuspensionFor(candidate, now), true
}

// RecordResourceSuccess clears the suspension history of a concrete profile
// whose attempt produced real output.
func (r *ProfileExecutionResolver) RecordResourceSuccess(ctx context.Context, executionProfileID string) {
	candidate, ok := r.resourceCandidate(ctx, executionProfileID)
	if !ok {
		return
	}
	r.engine.RecordResourceSuccess(candidate)
}

// RecordResourceFailure suspends a concrete profile's resource after a failure
// that is not routed to a successor.
func (r *ProfileExecutionResolver) RecordResourceFailure(
	ctx context.Context,
	executionProfileID string,
	failure *routingerr.Error,
) {
	candidate, ok := r.resourceCandidate(ctx, executionProfileID)
	if !ok || failure == nil {
		return
	}
	profile := dynamic.Profile{ID: executionProfileID, Candidates: []dynamic.Candidate{candidate}}
	r.engine.RecordResourceFailure(ctx, profile, candidate.ID, failure)
}
