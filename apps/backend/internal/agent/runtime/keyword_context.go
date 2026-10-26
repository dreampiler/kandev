package runtime

import (
	"context"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
)

// SetBillingTypeResolver reuses the registered agent's live billing detection.
// It is configured during startup and used only by the opt-in classifier.
func (r *ProfileExecutionResolver) SetBillingTypeResolver(resolve func(string) string) {
	r.billingTypeResolver = resolve
}

func (r *ProfileExecutionResolver) profileWithBillingType(ctx context.Context, profile *agentsettingsmodels.AgentProfile) *agentsettingsmodels.AgentProfile {
	if profile == nil || !r.keywordContextEnabled.Load() || r.billingTypeResolver == nil {
		return profile
	}
	copyProfile := *profile
	copyProfile.BillingType = ""
	name, err := r.agentNameForProfile(ctx, profile)
	if err == nil && name != "" {
		copyProfile.BillingType = r.billingTypeResolver(name)
	}
	return &copyProfile
}

func (r *ProfileExecutionResolver) populateKeywordCandidate(ctx context.Context, profile *agentsettingsmodels.AgentProfile, candidate *dynamic.Candidate) {
	if profile == nil || !candidate.KeywordContextEnabled {
		return
	}
	candidate.ModelID = profile.Model
	if r.bindingResolver != nil {
		profile = r.profileWithBillingType(ctx, profile)
		candidate.KeywordAccountID = r.bindingResolver.Fingerprint(profileCredentialBindingDescriptor(profile))
	}
}

// ProviderKeywordContext reuses the existing credential-binding identity.
// Missing billing or credential evidence cannot establish account scope.
func (r *ProfileExecutionResolver) ProviderKeywordContext(ctx context.Context, profileID string) routingerr.KeywordContext {
	var result routingerr.KeywordContext
	if r == nil || r.profiles == nil {
		return result
	}
	profile, err := r.profiles.GetAgentProfile(ctx, profileID)
	if err != nil || profile == nil {
		return result
	}
	profile = r.profileWithBillingType(ctx, profile)
	result.ModelID = profile.Model
	result.PathID = profile.AgentID
	result.Tier = profile.BillingType
	if r.bindingResolver != nil {
		result.AccountID = r.bindingResolver.Fingerprint(profileCredentialBindingDescriptor(profile))
	}
	return result
}
