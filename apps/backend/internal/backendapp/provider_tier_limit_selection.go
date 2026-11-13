package backendapp

import (
	"context"
	"errors"
	"strings"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

var errTierSelectionUnavailable = errors.New("execution model and account do not match the configured allowance")

func (a *usageProviderAdapter) bindSelectedModel(ctx context.Context, profile *settingsmodels.AgentProfile, agentType string, original usageBinding, selected *agentusage.ExecutionUsagePolicy) (*settingsmodels.AgentProfile, usageBinding, error) {
	if !agentusage.MatchesSelectedUsage(ctx, selected) {
		return nil, usageBinding{}, errTierSelectionUnavailable
	}
	copy := *profile
	copy.Model = selected.ExpectedModel
	binding, ok := a.bindingFor(&copy, agentType)
	if !ok || binding.accountKey != original.accountKey {
		return nil, usageBinding{}, errTierSelectionUnavailable
	}
	return &copy, binding, nil
}

func (s *providerTierLimitService) contextForCandidate(ctx context.Context, candidate dynamicruntime.Candidate) (tierLimitContext, error) {
	policy := candidate.UsagePolicy
	if policy == nil {
		if candidate.RequireExecutionUsage {
			return tierLimitContext{}, errTierSelectionUnavailable
		}
		return s.contextFor(ctx, candidate.ID)
	}
	if policy.ProfileID != candidate.ID || policy.ExpectedModel == "" ||
		policy.ExpectedProvider == "" || policy.ExpectedAccountRef == "" {
		return tierLimitContext{}, errTierSelectionUnavailable
	}
	return s.contextForSelection(ctx, candidate.ID, policy)
}

func tierUsageMatchesSelection(observed *agentusage.ProviderUsage, policy *agentusage.ExecutionUsagePolicy) bool {
	return observed != nil && !observed.Stale && strings.EqualFold(observed.Provider, policy.ExpectedProvider) &&
		observed.AccountRef == policy.ExpectedAccountRef &&
		observed.OrganizationRef == policy.ExpectedOrganizationRef
}
