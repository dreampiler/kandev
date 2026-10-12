package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ReviewerIdentity is the (agent CLI, model, mode) triple a review pass runs as.
// It is deliberately separate from the task's own agent profile: the point of the
// feature is that a different model can review than implemented.
type ReviewerIdentity struct {
	ProfileID string
	AgentID   string
	Model     string
	Mode      string
	// Source names where the identity came from, for the run record and logs:
	// "agent_profile", "utility_agent", or "user_default".
	Source string
}

// Identity sources.
const (
	SourceAgentProfile = "agent_profile"
	SourceUtilityAgent = "utility_agent"
	SourceUserDefault  = "user_default"
)

// ReviewerProfile is the subset of an agent profile the resolver needs.
type ReviewerProfile struct {
	AgentID        string
	Model          string
	Mode           string
	CLIPassthrough bool
	Name           string
}

// ProfileLookup resolves an agent profile ID. Returns found=false for an unknown
// profile.
type ProfileLookup interface {
	ReviewerProfile(ctx context.Context, profileID string) (profile ReviewerProfile, found bool, err error)
}

// UtilityAgentLookup resolves the built-in `code-review` utility agent, which
// supplies the default reviewer identity for the on-demand path.
type UtilityAgentLookup interface {
	// CodeReviewAgent returns the code-review utility agent's agent/model and
	// whether it is enabled. found=false means the row is missing entirely.
	CodeReviewAgent(ctx context.Context) (agentID, model string, enabled, found bool, err error)
}

type UtilityProfileLookup interface {
	CodeReviewAgentProfile(ctx context.Context) (profileID string, enabled, found bool, err error)
}

// DefaultsLookup resolves the user's default utility agent/model pair, used when
// the code-review utility agent leaves the model blank.
type DefaultsLookup interface {
	DefaultUtilitySettings(ctx context.Context) (agentID, model string, err error)
}

type DefaultProfileLookup interface {
	DefaultUtilityProfileID(ctx context.Context) (string, error)
}

// OriginIdentity is the concrete execution that produced the changes a review
// pass is judging. The workflow-step path sets it from the entering session so
// the resolver never selects the writer's own model or profile as the reviewer.
// Every field is optional; an empty value never matches a candidate.
type OriginIdentity struct {
	// ProfileID is the origin session's logical agent profile.
	ProfileID string
	// ExecutionProfileID is the concrete profile the origin actually ran under.
	ExecutionProfileID string
}

// Resolver picks the reviewer identity for a run.
type Resolver struct {
	Profiles ProfileLookup
	Utility  UtilityAgentLookup
	Defaults DefaultsLookup
}

// NewResolver builds a resolver. Any lookup may be nil; a nil lookup simply
// contributes no candidate identity.
func NewResolver(profiles ProfileLookup, utility UtilityAgentLookup, defaults DefaultsLookup) *Resolver {
	return &Resolver{Profiles: profiles, Utility: utility, Defaults: defaults}
}

// Resolve returns the reviewer identity for a run. See ResolveExcluding.
func (r *Resolver) Resolve(ctx context.Context, agentProfileID string) (ReviewerIdentity, error) {
	return r.ResolveExcluding(ctx, agentProfileID, nil)
}

// ResolveExcluding returns the reviewer identity for a run and removes any
// candidate that is the origin's own execution.
//
// Precedence is unchanged:
//  1. an explicit agent profile (the workflow-step path), which is what makes a
//     per-step reviewing model possible;
//  2. the enabled built-in `code-review` utility agent;
//  3. the user's default utility agent/model.
//
// When origin is non-nil, a candidate whose concrete profile ID is the origin's,
// or whose {agent, model, mode} triple equals the origin's, is skipped in place:
// the order and eligibility of every remaining candidate are preserved. A
// candidate the origin does not match still wins at its own precedence. When no
// distinct candidate remains, the resolver fails closed with ErrAgentUnavailable
// rather than silently reviewing with the writer's own model.
//
// The agent/model pair is treated as inseparable: a default model from one
// provider must never be sent to another provider's ACP config, so a blank model
// pulls both halves from the defaults rather than just the model.
func (r *Resolver) ResolveExcluding(ctx context.Context, agentProfileID string, origin *OriginIdentity) (ReviewerIdentity, error) {
	exclusion := r.originExclusion(ctx, origin)

	if trimmed := strings.TrimSpace(agentProfileID); trimmed != "" {
		identity, err := r.resolveFromProfile(ctx, trimmed)
		if err != nil {
			return ReviewerIdentity{}, err
		}
		if !exclusion.matches(r.withTriple(ctx, identity)) {
			return identity, nil
		}
		// The explicit profile is the origin's own execution; fall through to
		// the utility/default candidates rather than reviewing with it.
	}

	identity, err := r.resolveFromUtilityAgent(ctx)
	switch {
	case err == nil:
		if !exclusion.matches(r.withTriple(ctx, identity)) {
			return identity, nil
		}
		// The utility reviewer is the origin; fall through to the defaults.
	case errors.Is(err, errDeferToDefaults):
		// Absent or disabled utility agent; fall through to the defaults.
	default:
		// A configured-but-inconsistent utility agent is a hard failure:
		// falling back there would quietly review with a provider/model pair
		// the user never chose.
		return ReviewerIdentity{}, err
	}

	identity, err = r.resolveFromDefaults(ctx)
	if err != nil {
		return ReviewerIdentity{}, err
	}
	if exclusion.matches(r.withTriple(ctx, identity)) {
		return ReviewerIdentity{}, fmt.Errorf("%w: %w", ErrAgentUnavailable, ErrNoDistinctReviewer)
	}
	return identity, nil
}

// ErrNoDistinctReviewer marks the case where every configured candidate is the
// origin's own model/profile. It is always wrapped with ErrAgentUnavailable so
// the run records the existing review_agent_unavailable code.
var ErrNoDistinctReviewer = errors.New("reviewer would be the origin's own model/profile")

// ErrOriginUnreadable marks a workflow-step review whose writer identity could
// not be read. Without the origin the resolver cannot prove the reviewer is
// distinct, so it fails closed rather than risk reviewing with the writer's own
// model. Always wrapped with ErrAgentUnavailable.
var ErrOriginUnreadable = errors.New("the origin execution identity is unavailable")

// ResolveRequiredOrigin is the workflow-step form of ResolveExcluding: the
// caller knows a change was written and must exclude its origin execution. A nil
// origin means the writer's identity could not be read, which fails closed with
// ErrAgentUnavailable instead of selecting an indistinguishable reviewer.
func (r *Resolver) ResolveRequiredOrigin(ctx context.Context, agentProfileID string, origin *OriginIdentity) (ReviewerIdentity, error) {
	if origin == nil {
		return ReviewerIdentity{}, fmt.Errorf("%w: %w", ErrAgentUnavailable, ErrOriginUnreadable)
	}
	return r.ResolveExcluding(ctx, agentProfileID, origin)
}

// originExclusion is the set of identities the resolver must never select. It
// holds the origin's profile IDs and, when known, its concrete {agent, model,
// mode} triple.
type originExclusion struct {
	profileIDs map[string]bool
	agentID    string
	model      string
}

func (r *Resolver) originExclusion(ctx context.Context, origin *OriginIdentity) originExclusion {
	ex := originExclusion{profileIDs: map[string]bool{}}
	if origin == nil {
		return ex
	}
	if origin.ProfileID != "" {
		ex.profileIDs[origin.ProfileID] = true
	}
	if origin.ExecutionProfileID != "" {
		ex.profileIDs[origin.ExecutionProfileID] = true
	}
	// Prefer the concrete execution profile for the triple, then the logical
	// profile. A load failure leaves the triple empty; profile-ID equality
	// still applies.
	for _, id := range []string{origin.ExecutionProfileID, origin.ProfileID} {
		if id == "" || r.Profiles == nil {
			continue
		}
		profile, found, err := r.Profiles.ReviewerProfile(ctx, id)
		if err == nil && found {
			ex.agentID, ex.model = profile.AgentID, profile.Model
			break
		}
	}
	return ex
}

func (ex originExclusion) matches(identity ReviewerIdentity) bool {
	if identity.ProfileID != "" && ex.profileIDs[identity.ProfileID] {
		return true
	}
	if ex.agentID == "" || ex.model == "" || identity.AgentID == "" || identity.Model == "" {
		return false
	}
	// Same agent and model is the same underlying model, regardless of
	// permission mode: the owner's rule is that the reviewer must not silently
	// become the writer's own model, and a profile's mode is a posture, not a
	// distinct model. An empty mode on either side therefore still matches.
	return identity.AgentID == ex.agentID && identity.Model == ex.model
}

// withTriple fills in a profile-based candidate's {agent, model, mode} so it can
// be compared against the origin triple. Agent/model candidates already carry it.
func (r *Resolver) withTriple(ctx context.Context, identity ReviewerIdentity) ReviewerIdentity {
	if identity.AgentID != "" || identity.ProfileID == "" || r.Profiles == nil {
		return identity
	}
	profile, found, err := r.Profiles.ReviewerProfile(ctx, identity.ProfileID)
	if err == nil && found {
		identity.AgentID = profile.AgentID
		identity.Model = profile.Model
		identity.Mode = profile.Mode
	}
	return identity
}

// errDeferToDefaults marks the "code-review utility agent is absent or turned
// off" case, where falling back to the user's default agent/model is correct.
var errDeferToDefaults = errors.New("defer to default utility settings")

func (r *Resolver) resolveFromProfile(ctx context.Context, profileID string) (ReviewerIdentity, error) {
	if r.Profiles == nil {
		return ReviewerIdentity{}, fmt.Errorf("%w: agent profiles are not available", ErrAgentUnavailable)
	}
	profile, found, err := r.Profiles.ReviewerProfile(ctx, profileID)
	if err != nil {
		return ReviewerIdentity{}, fmt.Errorf("%w: load agent profile %s: %v", ErrAgentUnavailable, profileID, err)
	}
	if !found {
		return ReviewerIdentity{}, fmt.Errorf("%w: agent profile %s no longer exists", ErrAgentUnavailable, profileID)
	}
	if profile.CLIPassthrough {
		return ReviewerIdentity{}, fmt.Errorf(
			"%w: agent profile %q is CLI-passthrough only and cannot run a structured review",
			ErrAgentUnavailable, describeProfile(profile, profileID))
	}
	if profile.AgentID == "" {
		return ReviewerIdentity{}, fmt.Errorf("%w: agent profile %q has no agent configured",
			ErrAgentUnavailable, describeProfile(profile, profileID))
	}
	model := profile.Model
	if model == "" {
		// The profile names an agent but no model; borrow the user's default
		// model only when it belongs to the same agent.
		model = r.sameAgentDefaultModel(ctx, profile.AgentID)
	}
	if model == "" {
		return ReviewerIdentity{}, fmt.Errorf("%w: agent profile %q has no model configured",
			ErrAgentUnavailable, describeProfile(profile, profileID))
	}
	return ReviewerIdentity{
		ProfileID: profileID,
		AgentID:   profile.AgentID,
		Model:     model,
		Mode:      profile.Mode,
		Source:    SourceAgentProfile,
	}, nil
}

func describeProfile(profile ReviewerProfile, profileID string) string {
	if profile.Name != "" {
		return profile.Name
	}
	return profileID
}

// sameAgentDefaultModel returns the user's default model only when the default
// belongs to agentID, so a model is never crossed between providers.
func (r *Resolver) sameAgentDefaultModel(ctx context.Context, agentID string) string {
	if r.Defaults == nil {
		return ""
	}
	defaultAgent, defaultModel, err := r.Defaults.DefaultUtilitySettings(ctx)
	if err != nil || defaultAgent != agentID {
		return ""
	}
	return defaultModel
}

func (r *Resolver) resolveFromUtilityAgent(ctx context.Context) (ReviewerIdentity, error) {
	if profileLookup, ok := r.Utility.(UtilityProfileLookup); ok {
		profileID, enabled, found, err := profileLookup.CodeReviewAgentProfile(ctx)
		if err != nil {
			return ReviewerIdentity{}, fmt.Errorf("%w: load code-review utility profile: %v", ErrAgentUnavailable, err)
		}
		if !found || !enabled || profileID == "" {
			return ReviewerIdentity{}, fmt.Errorf("%w: code-review utility profile is not configured", errDeferToDefaults)
		}
		return ReviewerIdentity{ProfileID: profileID, Source: SourceUtilityAgent}, nil
	}
	if r.Utility == nil {
		return ReviewerIdentity{}, fmt.Errorf("%w: %w: no code-review utility agent is wired",
			ErrAgentUnavailable, errDeferToDefaults)
	}
	agentID, model, enabled, found, err := r.Utility.CodeReviewAgent(ctx)
	if err != nil {
		// A lookup failure is a real fault, not an unconfigured feature; do not
		// mask it by silently reviewing with the defaults.
		return ReviewerIdentity{}, fmt.Errorf("%w: load code-review utility agent: %v", ErrAgentUnavailable, err)
	}
	if !found {
		return ReviewerIdentity{}, fmt.Errorf("%w: %w: the code-review utility agent is missing",
			ErrAgentUnavailable, errDeferToDefaults)
	}
	if !enabled {
		return ReviewerIdentity{}, fmt.Errorf("%w: %w: the code-review utility agent is disabled",
			ErrAgentUnavailable, errDeferToDefaults)
	}
	// A configured utility agent with a blank model means "use my defaults";
	// take the agent and model together so the pair stays consistent.
	if model == "" {
		defaults, defErr := r.resolveFromDefaults(ctx)
		if defErr != nil {
			return ReviewerIdentity{}, defErr
		}
		if agentID == "" {
			agentID = defaults.AgentID
		}
		if agentID != defaults.AgentID {
			return ReviewerIdentity{}, fmt.Errorf(
				"%w: the code-review utility agent uses %s but the default model belongs to %s; set a model on the code-review agent in Settings > Utility Agents",
				ErrAgentUnavailable, agentID, defaults.AgentID)
		}
		return ReviewerIdentity{AgentID: agentID, Model: defaults.Model, Source: SourceUtilityAgent}, nil
	}
	if agentID == "" {
		return ReviewerIdentity{}, fmt.Errorf("%w: the code-review utility agent has no agent configured", ErrAgentUnavailable)
	}
	return ReviewerIdentity{AgentID: agentID, Model: model, Source: SourceUtilityAgent}, nil
}

func (r *Resolver) resolveFromDefaults(ctx context.Context) (ReviewerIdentity, error) {
	if profileLookup, ok := r.Defaults.(DefaultProfileLookup); ok {
		profileID, err := profileLookup.DefaultUtilityProfileID(ctx)
		if err != nil {
			return ReviewerIdentity{}, fmt.Errorf("%w: load default utility profile: %v", ErrAgentUnavailable, err)
		}
		if profileID == "" {
			return ReviewerIdentity{}, fmt.Errorf("%w: no default utility profile configured", ErrAgentUnavailable)
		}
		return ReviewerIdentity{ProfileID: profileID, Source: SourceUserDefault}, nil
	}
	if r.Defaults == nil {
		return ReviewerIdentity{}, fmt.Errorf("%w: no default utility agent is configured", ErrAgentUnavailable)
	}
	agentID, model, err := r.Defaults.DefaultUtilitySettings(ctx)
	if err != nil {
		return ReviewerIdentity{}, fmt.Errorf("%w: load default utility settings: %v", ErrAgentUnavailable, err)
	}
	if agentID == "" || model == "" {
		return ReviewerIdentity{}, fmt.Errorf(
			"%w: no inference-capable agent and model are configured; set one in Settings > Utility Agents",
			ErrAgentUnavailable)
	}
	return ReviewerIdentity{AgentID: agentID, Model: model, Source: SourceUserDefault}, nil
}
