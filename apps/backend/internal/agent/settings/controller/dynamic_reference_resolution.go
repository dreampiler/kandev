package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/agent/settings/dto"
	"github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/agent/settings/store"
)

// The resolution operation/action pairs are a closed set. `delete` never pairs
// with `disable_candidate`: a disabled candidate reference must not outlive the
// profile row it points at.
const (
	dynamicResolutionOperationDisable = "disable"
	dynamicResolutionOperationDelete  = "delete"
	dynamicResolutionActionDisable    = "disable_candidate"
	dynamicResolutionActionRemove     = "remove_candidate"
)

// ErrDynamicResolutionInvalid marks a malformed resolution request: an unknown
// operation or action, a missing version map, or the rejected delete +
// disable_candidate combination. It maps to a field-addressable 400 before any
// write.
var ErrDynamicResolutionInvalid = errors.New("invalid dynamic reference resolution request")

// ErrDynamicProfileWouldEmpty marks a remove_candidate that would leave a
// referencing dynamic profile with no candidate rows at all. An empty route
// document is not a valid dynamic profile, so the whole request is rejected
// before any write, naming the profile that would be emptied. It maps to the
// existing conflict response.
var ErrDynamicProfileWouldEmpty = errors.New("removing the candidate would leave a dynamic profile with no candidates")

// DynamicReferenceResolutionRequest resolves every dynamic profile that
// references a concrete profile before that profile is disabled or deleted.
type DynamicReferenceResolutionRequest struct {
	ProfileID string
	Operation string
	Action    string
	Versions  map[string]int64
}

// ResolveDynamicReferences applies the requested action to every dynamic
// profile that references the target profile and disables or deletes the
// target, all in one store transaction. A version mismatch, a changed
// reference set, or any other failure leaves every row at its pre-request
// state. Routing-tier, utility-agent, session, watcher, and automation limits
// keep their existing block behavior; this path never sets force.
func (c *Controller) ResolveDynamicReferences(
	ctx context.Context,
	req DynamicReferenceResolutionRequest,
) (*dto.AgentProfileDTO, error) {
	if err := validateDynamicResolutionRequest(req); err != nil {
		return nil, err
	}
	resolver, ok := c.repo.(store.DynamicReferenceResolutionRepository)
	if !ok {
		return nil, fmt.Errorf("dynamic reference resolution is unavailable")
	}
	profile, err := c.repo.GetAgentProfile(ctx, req.ProfileID)
	if err != nil {
		if isProfileNotFoundErr(err) {
			return nil, ErrAgentProfileNotFound
		}
		return nil, err
	}
	if profileKind(profile) == dynamicProfileKind && !c.dynamicAgentRoutingEnabled {
		return nil, ErrDynamicAgentRoutingDisabled
	}
	resolutions, err := c.buildDynamicResolutions(ctx, req)
	if err != nil {
		return nil, err
	}
	if req.Operation == dynamicResolutionOperationDelete {
		return c.resolveAndDeleteProfile(ctx, resolver, profile, resolutions)
	}
	return c.resolveAndDisableProfile(ctx, resolver, profile, resolutions)
}

func (c *Controller) resolveAndDisableProfile(
	ctx context.Context,
	resolver store.DynamicReferenceResolutionRepository,
	profile *models.AgentProfile,
	resolutions []store.DynamicProfileResolution,
) (*dto.AgentProfileDTO, error) {
	if err := c.checkDisableReferences(ctx, profile.ID); err != nil {
		return nil, err
	}
	disabled := false
	if err := resolver.ResolveDynamicReferencesAndUpdateProfile(ctx, profile, &disabled, resolutions); err != nil {
		return nil, err
	}
	result := toProfileDTO(profile)
	c.decorateProviderSupportByAgentID(ctx, &result)
	return &result, nil
}

func (c *Controller) resolveAndDeleteProfile(
	ctx context.Context,
	resolver store.DynamicReferenceResolutionRepository,
	profile *models.AgentProfile,
	resolutions []store.DynamicProfileResolution,
) (*dto.AgentProfileDTO, error) {
	if err := c.checkDeleteReferences(ctx, profile.ID); err != nil {
		return nil, err
	}
	// Automations are disabled before the row is removed and a failure aborts
	// the delete, mirroring DeleteProfile's ordering.
	if err := c.disableReferencingAutomations(ctx, profile.ID); err != nil {
		return nil, err
	}
	if err := resolver.ResolveDynamicReferencesAndDeleteProfile(ctx, profile.ID, resolutions); err != nil {
		return nil, err
	}
	c.cleanupEphemeralTasks(ctx, profile.ID)
	// Watchers are disabled after the row is gone and best-effort, mirroring
	// DeleteProfile.
	c.disableReferencingWatchers(ctx, profile.ID, profile.Name)
	result := toProfileDTO(profile)
	return &result, nil
}

func validateDynamicResolutionRequest(req DynamicReferenceResolutionRequest) error {
	if strings.TrimSpace(req.ProfileID) == "" {
		return fmt.Errorf("%w: profile id is required", ErrDynamicResolutionInvalid)
	}
	switch req.Operation {
	case dynamicResolutionOperationDisable, dynamicResolutionOperationDelete:
	default:
		return fmt.Errorf("%w: operation must be disable or delete", ErrDynamicResolutionInvalid)
	}
	switch req.Action {
	case dynamicResolutionActionDisable, dynamicResolutionActionRemove:
	default:
		return fmt.Errorf("%w: action must be disable_candidate or remove_candidate", ErrDynamicResolutionInvalid)
	}
	if req.Operation == dynamicResolutionOperationDelete && req.Action == dynamicResolutionActionDisable {
		return fmt.Errorf("%w: delete cannot retain a disabled candidate reference", ErrDynamicResolutionInvalid)
	}
	if len(req.Versions) == 0 {
		return fmt.Errorf("%w: versions are required", ErrDynamicResolutionInvalid)
	}
	return nil
}

// buildDynamicResolutions reads the current references, verifies every echoed
// version, and builds each referencing dynamic profile's replacement document.
// It fails closed on a new/removed reference or any version mismatch before the
// store transaction runs; the store re-verifies the same set inside the
// transaction so a concurrent writer cannot slip through.
func (c *Controller) buildDynamicResolutions(
	ctx context.Context,
	req DynamicReferenceResolutionRequest,
) ([]store.DynamicProfileResolution, error) {
	dynamicRepo, err := c.dynamicProfileRepository()
	if err != nil {
		return nil, err
	}
	refs, err := c.listDynamicProfileReferences(ctx, req.ProfileID)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("%w: profile has no dynamic references", ErrDynamicResolutionInvalid)
	}
	if len(req.Versions) != len(refs) {
		return nil, ErrDynamicProfileVersionConflict
	}
	resolutions := make([]store.DynamicProfileResolution, 0, len(refs))
	for _, ref := range refs {
		expected, ok := req.Versions[ref.ID]
		if !ok || expected != ref.Version {
			return nil, ErrDynamicProfileVersionConflict
		}
		profile, routes, err := dynamicRepo.GetDynamicAgentProfile(ctx, ref.ID)
		if err != nil {
			return nil, err
		}
		if profile.Version != expected {
			return nil, ErrDynamicProfileVersionConflict
		}
		newRoutes, changed, err := resolveDynamicCandidateRoutes(routes, req.ProfileID, req.Action)
		if err != nil {
			return nil, nameDynamicResolutionError(ref, err)
		}
		if !changed {
			return nil, fmt.Errorf("%w: dynamic profile %s no longer references this profile", ErrDynamicResolutionInvalid, ref.ID)
		}
		resolutions = append(resolutions, store.DynamicProfileResolution{
			ProfileID:             ref.ID,
			ExpectedVersion:       profile.Version,
			KeepModelWhileRunning: profile.KeepModelWhileRunning,
			Routes:                newRoutes,
		})
	}
	return resolutions, nil
}

// resolveDynamicCandidateRoutes maps the requested action onto the route
// document. `disable_candidate` keeps every row and clears the candidate's
// enabled flag; `remove_candidate` removes the row and, when it was a tier
// head, promotes the next same-tier row. Removing the profile's only candidate
// row would leave an invalid empty document, so it fails closed with
// ErrDynamicProfileWouldEmpty before any write.
func resolveDynamicCandidateRoutes(
	routes []models.DynamicAgentRoute,
	targetID, action string,
) ([]models.DynamicAgentRoute, bool, error) {
	if action == dynamicResolutionActionRemove {
		out, changed, err := removeDynamicCandidate(routes, targetID)
		if err != nil {
			return nil, false, err
		}
		if changed && len(out) == 0 {
			return nil, false, ErrDynamicProfileWouldEmpty
		}
		return out, changed, nil
	}
	disabled, matched := disableDynamicCandidate(routes, targetID)
	return disabled, matched, nil
}

// nameDynamicResolutionError attaches the referencing dynamic profile's name to
// a would-empty rejection so the administrator sees which profile blocks the
// change. Every other error passes through unchanged.
func nameDynamicResolutionError(ref DynamicProfileReference, err error) error {
	if errors.Is(err, ErrDynamicProfileWouldEmpty) {
		return fmt.Errorf("%w: %q", ErrDynamicProfileWouldEmpty, ref.Name)
	}
	return err
}

// disableDynamicCandidate clears the candidate's enabled flag and reports
// whether it was present, so a document that no longer references the profile
// fails closed instead of being rewritten as a no-op.
func disableDynamicCandidate(
	routes []models.DynamicAgentRoute,
	targetID string,
) ([]models.DynamicAgentRoute, bool) {
	out := make([]models.DynamicAgentRoute, len(routes))
	copy(out, routes)
	matched := false
	for index := range out {
		if out[index].ExecutionProfileID == targetID {
			out[index].Enabled = false
			matched = true
		}
	}
	return out, matched
}

// removeDynamicCandidate removes the candidate row, renumbers positions, and
// when the removed row owned a tier that the next row joined, promotes that
// next row to head and copies the tier's mode/on_failure onto it. It never
// merges tiers or reorders other candidates.
func removeDynamicCandidate(
	routes []models.DynamicAgentRoute,
	targetID string,
) ([]models.DynamicAgentRoute, bool, error) {
	index := dynamicRouteIndex(routes, targetID)
	if index < 0 {
		return routes, false, nil
	}
	policies, err := decodeRoutePolicies(routes)
	if err != nil {
		return nil, false, err
	}
	isHead, headTier := routeTierHead(policies, index)
	inherits := isHead && routeJoinsPrevious(policies, index+1)

	out := make([]models.DynamicAgentRoute, 0, len(routes)-1)
	for i, route := range routes {
		if i == index {
			continue
		}
		route.Position = len(out)
		out = append(out, route)
	}
	if inherits {
		if err := inheritTierHead(&out[index], policies[index+1], headTier); err != nil {
			return nil, false, err
		}
	}
	return out, true, nil
}

func dynamicRouteIndex(routes []models.DynamicAgentRoute, targetID string) int {
	for i := range routes {
		if routes[i].ExecutionProfileID == targetID {
			return i
		}
	}
	return -1
}

func decodeRoutePolicies(routes []models.DynamicAgentRoute) ([]dto.DynamicAgentPolicyDTO, error) {
	policies := make([]dto.DynamicAgentPolicyDTO, len(routes))
	for i, route := range routes {
		policy, err := decodeDynamicPolicyDocument(route.RulesJSON, route.Position)
		if err != nil {
			return nil, err
		}
		policies[i] = policy
	}
	return policies, nil
}

// routeTierHead reports whether the row at index owns its tier and, if so, the
// normalized tier policy it owns. A joined row owns only its model options.
func routeTierHead(
	policies []dto.DynamicAgentPolicyDTO,
	index int,
) (bool, dto.DynamicAgentTierPolicyDTO) {
	policy := policies[index]
	if index != 0 && policy.Selection != nil && policy.Selection.JoinPrevious {
		return false, defaultDynamicTierPolicy()
	}
	tier := defaultDynamicTierPolicy()
	if policy.Selection != nil && policy.Selection.Tier != nil {
		tier = normalizedDynamicTierPolicy(*policy.Selection.Tier)
	}
	return true, tier
}

func routeJoinsPrevious(policies []dto.DynamicAgentPolicyDTO, index int) bool {
	if index >= len(policies) {
		return false
	}
	policy := policies[index]
	return policy.Selection != nil && policy.Selection.JoinPrevious
}

// inheritTierHead makes a promoted follower own the removed head's tier: it
// clears join_previous and stamps the head's mode/on_failure while preserving
// the row's own model options.
func inheritTierHead(
	follower *models.DynamicAgentRoute,
	policy dto.DynamicAgentPolicyDTO,
	tier dto.DynamicAgentTierPolicyDTO,
) error {
	if policy.Selection == nil {
		policy.Selection = &dto.DynamicAgentSelectionDTO{}
	}
	policy.Selection.JoinPrevious = false
	tierCopy := tier
	policy.Selection.Tier = &tierCopy
	raw, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal inherited dynamic route policy: %w", err)
	}
	follower.RulesJSON = string(raw)
	return nil
}

// checkDisableReferences applies the disable path's non-dynamic limits. A
// utility-agent reference keeps its existing confirmation; routing tiers are
// not a disable blocker, matching UpdateProfile.
func (c *Controller) checkDisableReferences(ctx context.Context, profileID string) error {
	if c.utilityDeps == nil {
		return nil
	}
	refs, err := c.utilityDeps.ListUtilityAgentsByAgentProfile(ctx, profileID)
	if err != nil {
		return fmt.Errorf("check utility agents using this profile: %w", err)
	}
	if len(refs) > 0 {
		return &ErrProfileInUseDetail{UtilityAgents: refs}
	}
	return nil
}

// checkDeleteReferences applies the non-force delete limits to the resolution
// delete path. The dynamic references are already resolved, so they are not
// reported; every other reference keeps its block behavior.
func (c *Controller) checkDeleteReferences(ctx context.Context, profileID string) error {
	blockers, err := c.profileReferenceBlockers(ctx, profileID, false)
	if err != nil {
		return err
	}
	if blockers != nil {
		return blockers
	}
	return nil
}

// profileReferenceBlockers gathers the non-dynamic references that block a
// delete. Routing tiers are a hard blocker even under force; force skips the
// soft session/watcher/automation/utility blockers. Dynamic references are not
// included: callers handle them explicitly.
func (c *Controller) profileReferenceBlockers(
	ctx context.Context,
	profileID string,
	force bool,
) (*ErrProfileInUseDetail, error) {
	routingTierRefs, err := c.listRoutingTierReferences(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if len(routingTierRefs) > 0 {
		return &ErrProfileInUseDetail{RoutingTiers: routingTierRefs}, nil
	}
	utilityRefs, err := c.utilityReferences(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if force {
		return nil, nil
	}
	return c.softDeletionBlockers(ctx, profileID, utilityRefs)
}

// softDeletionBlockers returns the non-force delete blockers that keep their
// confirmation: utility agents when there is no session checker, otherwise the
// combined active sessions, watchers, automations, and utility agents.
func (c *Controller) softDeletionBlockers(
	ctx context.Context,
	profileID string,
	utilityRefs []UtilityAgentReference,
) (*ErrProfileInUseDetail, error) {
	if c.sessionChecker == nil {
		if len(utilityRefs) > 0 {
			return &ErrProfileInUseDetail{UtilityAgents: utilityRefs}, nil
		}
		return nil, nil
	}
	activeTasks, err := c.sessionChecker.GetActiveTaskInfoByAgentProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	watcherRefs := c.watcherReferences(ctx, profileID)
	automationRefs, err := c.automationReferences(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if len(activeTasks) > 0 || len(watcherRefs) > 0 || len(automationRefs) > 0 || len(utilityRefs) > 0 {
		return &ErrProfileInUseDetail{
			ActiveSessions: activeTasks,
			Watchers:       watcherRefs,
			Automations:    automationRefs,
			UtilityAgents:  utilityRefs,
		}, nil
	}
	return nil, nil
}

func (c *Controller) utilityReferences(
	ctx context.Context,
	profileID string,
) ([]UtilityAgentReference, error) {
	if c.utilityDeps == nil {
		return nil, nil
	}
	refs, err := c.utilityDeps.ListUtilityAgentsByAgentProfile(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("check utility agents using this profile: %w", err)
	}
	return refs, nil
}

// watcherReferences is best-effort: a lookup failure is logged and treated as
// no watchers, matching DeleteProfile, because watchers have a preflight
// backstop that repairs the row on the next poll.
func (c *Controller) watcherReferences(ctx context.Context, profileID string) []WatcherReference {
	if c.watcherDeps == nil {
		return nil
	}
	refs, err := c.watcherDeps.ListWatchersByAgentProfile(ctx, profileID)
	if err != nil {
		c.logger.Warn("watcher deps lookup failed; proceeding without watcher info",
			zap.String("profile_id", profileID), zap.Error(err))
		return nil
	}
	return refs
}

func (c *Controller) automationReferences(
	ctx context.Context,
	profileID string,
) ([]AutomationReference, error) {
	if c.automationDeps == nil {
		return nil, nil
	}
	refs, err := c.automationDeps.ListEnabledAutomationsByAgentProfile(ctx, profileID)
	if err != nil {
		// Fails closed: a lookup that silently returned nothing would orphan
		// exactly the automations this check exists to catch.
		return nil, fmt.Errorf("check automations using this profile: %w", err)
	}
	return refs, nil
}
