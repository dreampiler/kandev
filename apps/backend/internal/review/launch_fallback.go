package review

import (
	"context"
	"errors"
	"fmt"
)

type unavailableRoute struct {
	DynamicRouteSelector
	profiles map[string]bool
}

func (r unavailableRoute) SelectCandidate(ctx context.Context, profileID string, skip func(string) bool) (string, error) {
	return r.DynamicRouteSelector.SelectCandidate(ctx, profileID, func(id string) bool {
		return r.profiles[id] || (skip != nil && skip(id))
	})
}

func (r *Resolver) nextDynamicReviewer(ctx context.Context, current ReviewerIdentity, origin *OriginIdentity, unavailable map[string]bool) (ReviewerIdentity, error) {
	if current.DynamicProfileID == "" || r.Routes == nil {
		return ReviewerIdentity{}, ErrAgentUnavailable
	}
	profile, err := r.loadProfile(ctx, current.DynamicProfileID)
	if err != nil {
		return ReviewerIdentity{}, err
	}
	filtered := *r
	filtered.Routes = unavailableRoute{DynamicRouteSelector: r.Routes, profiles: unavailable}
	next, ok, err := filtered.resolveDynamic(ctx, current.DynamicProfileID, profile, current.Source, r.originExclusion(ctx, origin))
	if err != nil {
		return ReviewerIdentity{}, err
	}
	if !ok {
		return ReviewerIdentity{}, fmt.Errorf("%w: no remaining executable reviewer", ErrAgentUnavailable)
	}
	return next, nil
}

func (r *Runner) reviewWithLaunchFallback(ctx context.Context, plan BatchPlan, identity ReviewerIdentity, req RunRequest, promptCtx PromptContext) (accumulator, ReviewerIdentity, error) {
	unavailable := map[string]bool{}
	for {
		acc, err := r.reviewBatches(ctx, plan, identity, req.SessionID, promptCtx)
		if err == nil || !errors.Is(err, ErrAgentUnavailable) || identity.DynamicProfileID == "" || ctx.Err() != nil {
			return acc, identity, err
		}
		unavailable[identity.ProfileID] = true
		next, resolveErr := r.resolver.nextDynamicReviewer(ctx, identity, req.Origin, unavailable)
		if resolveErr != nil {
			return acc, identity, fmt.Errorf("%w: %v", err, resolveErr)
		}
		identity = next
	}
}
