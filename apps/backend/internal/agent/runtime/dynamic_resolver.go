package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/runtime/routingpolicy"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/agent/settings/store"
)

var ErrDynamicRoutingDisabled = errors.New("dynamic agent routing is disabled")

const (
	dynamicRouteStatusRetrying = "retrying"
	dynamicRouteStatusWaiting  = "waiting"
)

// ProfileExecution is the caller-facing result of resolving a logical
// profile. Concrete callers receive the same ID for both fields. Dynamic
// callers retain their logical ID while the resolver records the concrete
// candidate that owns the downstream launch.
type ProfileExecution struct {
	LogicalProfileID   string
	ExecutionProfileID string
	AgentName          string
	RouteSessionID     string
	Generation         int64
	ProfileVersion     int64
	Profile            *agentsettingsmodels.AgentProfile
	Decision           dynamic.RouteDecision
}

// ProfileExecutionResolver is the shared profile-kind boundary. Callers pass
// one profile ID and never need to branch on the dynamic family themselves.
// It intentionally returns a route decision rather than launching an agent;
// the conductor/lifecycle layer owns downstream ACP sessions.
type ProfileExecutionResolver struct {
	profiles         store.Repository
	dynamic          store.DynamicProfileRepository
	engine           *dynamic.Engine
	bindingResolver  *dynamic.CredentialBindingResolver
	sessionExecutors SessionExecutorResolver
	providerLimits   ProviderLimitReader
	enabled          atomic.Bool
}

func NewProfileExecutionResolver(profiles store.Repository, engine *dynamic.Engine, enabled bool) *ProfileExecutionResolver {
	var dynamicRepo store.DynamicProfileRepository
	if repo, ok := profiles.(store.DynamicProfileRepository); ok {
		dynamicRepo = repo
	}
	resolver := &ProfileExecutionResolver{profiles: profiles, dynamic: dynamicRepo, engine: engine}
	resolver.enabled.Store(enabled)
	return resolver
}

func (r *ProfileExecutionResolver) SetEnabled(enabled bool) { r.enabled.Store(enabled) }

// Enabled reports the effective dynamic-agent-routing flag value this
// resolver was constructed or last set with. Callers outside this package
// use it to gate durable recovery and manual route-action launch paths that
// do not otherwise pass through a selection method.
func (r *ProfileExecutionResolver) Enabled() bool { return r.enabled.Load() }

// SetCredentialBindingResolver supplies the installation-scoped fingerprint
// used to share provider health between concrete profiles that prove the same
// credential binding. A missing or incomplete descriptor remains isolated to
// the concrete profile through the resolver's conservative fallback.
func (r *ProfileExecutionResolver) SetCredentialBindingResolver(resolver *dynamic.CredentialBindingResolver) {
	r.bindingResolver = resolver
}

// KeepModelWhileRunning reports the profile-wide continuity preference. A load
// failure answers true, the documented default, because failing open toward
// stickiness keeps an admitted turn on its current candidate rather than
// silently moving work when the preference could not be read.
func (r *ProfileExecutionResolver) KeepModelWhileRunning(
	ctx context.Context,
	logicalProfileID string,
) bool {
	if r == nil || !r.Enabled() {
		return true
	}
	profile, err := r.loadDynamicProfile(ctx, logicalProfileID)
	if err != nil {
		return true
	}
	return profile.KeepModelWhileRunning
}

// ReevaluateForNewTurn runs one fresh capacity comparison for a session whose
// profile does not keep its chosen model. The incumbent is not preferred, so the
// tier ranking decides which candidate a new user turn should use. The caller
// invokes it at the idle boundary before launching, so an admitted turn is never
// interrupted.
func (r *ProfileExecutionResolver) ReevaluateForNewTurn(
	ctx context.Context,
	sessionID string,
	logicalProfileID string,
	expectedGeneration int64,
	executorID string,
) (dynamic.RouteDecision, error) {
	if r == nil || r.engine == nil {
		return dynamic.RouteDecision{}, errors.New("dynamic routing is not configured")
	}
	profile, err := r.loadDynamicProfileForExecutor(ctx, logicalProfileID, executorID)
	if err != nil {
		return dynamic.RouteDecision{}, err
	}
	return r.engine.SelectContext(ctx, sessionID, profile, expectedGeneration, "")
}

// LoadDynamicProfileForExecutor loads the profile with the execution environment
// of the session that will run it. The executor lives on the task session, so
// this is how a remote execution is distinguished from a host one and kept from
// inheriting the host's provider account usage.
func (r *ProfileExecutionResolver) LoadDynamicProfileForExecutor(
	ctx context.Context,
	profileID string,
	executorID string,
) (dynamic.Profile, error) {
	return r.loadDynamicProfileForExecutor(ctx, profileID, executorID)
}

// LoadDynamicProfileForSession loads the profile with the execution environment
// resolved from the session. It is the form every selection entry point uses, so
// a session's executor reaches usage attribution without each caller threading it.
func (r *ProfileExecutionResolver) LoadDynamicProfileForSession(
	ctx context.Context,
	profileID string,
	sessionID string,
) (dynamic.Profile, error) {
	return r.loadDynamicProfileForSession(ctx, profileID, sessionID, "")
}

// executionEnvironment reports the candidate's execution environment for a
// session. The executor lives on the task session rather than the agent profile,
// so this is the only place it can be known authoritatively; an unqualified
// session is treated as the host default, which is what an unqualified profile
// launches on.
// executionEnvironment reports the candidate's execution environment for a
// session, using the single classification the runtime owns.
func executionEnvironment(executorID string) string {
	if dynamic.IsRemoteExecutor(executorID) {
		return strings.TrimSpace(executorID)
	}
	return "local"
}

// OpenCircuit reports whether a concrete candidate's own credential binding is
// currently paused. The settings preview uses it so a prediction accounts for
// route health exactly as a live selection does, rather than naming a candidate
// an actual selection would refuse.
//
// A candidate whose profile or binding cannot be resolved reports an unknown
// verdict instead of a healthy one: the caller must not present an unverified
// candidate as selectable.
func (r *ProfileExecutionResolver) OpenCircuit(
	ctx context.Context,
	executionProfileID string,
	now time.Time,
) (open bool, known bool) {
	suspension, known := r.CandidateSuspension(ctx, executionProfileID, now)
	return suspension.Blocked(), known
}

// NewConductor creates the lifecycle-facing conductor with the same engine,
// profile loader, and feature-gate state as this resolver.
func (r *ProfileExecutionResolver) NewConductor(
	downstream dynamic.DownstreamRuntime,
	options ...dynamic.ConductorOption,
) *dynamic.Conductor {
	if persistence := r.engine.ContinuationPersistence(); persistence != nil {
		options = append(options, dynamic.WithContinuationPersistence(persistence))
	}
	return dynamic.NewConductor(r.engine, r, downstream, options...)
}

// ValidateProfile performs the disabled-mode check without claiming a route
// generation or writing any durable state. Callers use it before creating a
// task session so a stored dynamic profile remains inert while the feature is
// disabled.
func (r *ProfileExecutionResolver) ValidateProfile(ctx context.Context, profileID string) error {
	if profileID == "" {
		// The ordinary launch path may resolve a workspace or workflow default
		// later. There is no profile family to gate until that resolution has
		// produced an ID.
		return nil
	}
	if r.profiles == nil {
		return errors.New("profile execution resolver has no profile store")
	}
	profile, err := r.profiles.GetAgentProfile(ctx, profileID)
	if err != nil {
		return fmt.Errorf("validate profile %s: %w", profileID, err)
	}
	agent, err := r.profiles.GetAgent(ctx, profile.AgentID)
	if err != nil {
		return fmt.Errorf("validate profile family %s: %w", profileID, err)
	}
	if agent.Name == agents.DynamicAgentID && !r.enabled.Load() {
		return ErrDynamicRoutingDisabled
	}
	return nil
}

// ResolveExecution preserves the small utility resolver contract for callers
// that do not carry a session identity.
func (r *ProfileExecutionResolver) ResolveExecution(ctx context.Context, profileID string) (*agentsettingsmodels.AgentProfile, string, error) {
	return r.ResolveExecutionForSession(ctx, "", profileID)
}

// LoadDynamicProfile implements dynamic.ProfileLoader for the conductor. It
// returns the same ordered, fail-closed candidate view used by ordinary
// profile resolution, without claiming a route generation.
func (r *ProfileExecutionResolver) LoadDynamicProfile(ctx context.Context, profileID string) (dynamic.Profile, error) {
	return r.loadDynamicProfile(ctx, profileID)
}

// ResolveExecutionForSession resolves one logical profile for a caller that
// has a durable session identity. Concrete profiles pass through unchanged;
// dynamic profiles claim a generation and return the selected concrete row.
func (r *ProfileExecutionResolver) ResolveExecutionForSession(ctx context.Context, sessionID, profileID string) (*agentsettingsmodels.AgentProfile, string, error) {
	execution, err := r.ResolveExecutionDetails(ctx, sessionID, profileID)
	if err != nil {
		return nil, "", err
	}
	return execution.Profile, execution.ExecutionProfileID, nil
}

// ResolveExecutionDetails returns the concrete profile and route metadata for
// a logical profile. Sessionless utility calls receive an isolated route ID.
func (r *ProfileExecutionResolver) ResolveExecutionDetails(ctx context.Context, sessionID, profileID string) (ProfileExecution, error) {
	profile, err := r.profiles.GetAgentProfile(ctx, profileID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve profile %s: %w", profileID, err)
	}
	agent, err := r.profiles.GetAgent(ctx, profile.AgentID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve profile family %s: %w", profileID, err)
	}
	if agent.Name != agents.DynamicAgentID {
		return ProfileExecution{LogicalProfileID: profileID, ExecutionProfileID: profile.ID, AgentName: agent.Name, Profile: profile}, nil
	}
	if !r.enabled.Load() {
		return ProfileExecution{}, ErrDynamicRoutingDisabled
	}
	// Utility calls always get an isolated route state, even when the template
	// context belongs to an already-routed task session. Reusing that session
	// would consume or fence the task's durable generation.
	routeSessionID := "utility:" + uuid.NewString()
	decision, err := r.Resolve(ctx, routeSessionID, profileID, 0, "")
	if err != nil {
		return ProfileExecution{}, err
	}
	decision.RouteSessionID = routeSessionID
	return decision, nil
}

// ResolveExecutionAfterFailure applies a classified prompt failure and
// returns the next concrete execution profile for the same logical route.
func (r *ProfileExecutionResolver) ResolveExecutionAfterFailure(
	ctx context.Context,
	sessionID, profileID, currentExecutionProfileID string,
	expectedGeneration int64,
	failure *routingerr.Error,
) (ProfileExecution, error) {
	if r.profiles == nil || r.engine == nil {
		return ProfileExecution{}, errors.New("dynamic profile execution is not configured")
	}
	if err := r.ValidateProfile(ctx, profileID); err != nil {
		return ProfileExecution{}, err
	}
	profile, err := r.profiles.GetAgentProfile(ctx, profileID)
	if err != nil {
		return ProfileExecution{}, err
	}
	agent, err := r.profiles.GetAgent(ctx, profile.AgentID)
	if err != nil {
		return ProfileExecution{}, err
	}
	if agent.Name != agents.DynamicAgentID {
		return ProfileExecution{LogicalProfileID: profileID, ExecutionProfileID: profile.ID, AgentName: agent.Name, Profile: profile}, nil
	}
	if sessionID == "" {
		sessionID = "utility:" + uuid.NewString()
	}
	profileConfig, err := r.loadDynamicProfileForSession(ctx, profileID, sessionID, "")
	if err != nil {
		return ProfileExecution{}, err
	}
	decision, err := r.engine.ApplyFailureContext(ctx, sessionID, profileConfig, expectedGeneration, currentExecutionProfileID, failure)
	if err != nil {
		return ProfileExecution{}, err
	}
	concrete, err := r.profiles.GetAgentProfile(ctx, decision.ExecutionProfileID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve execution profile %s: %w", decision.ExecutionProfileID, err)
	}
	concreteAgentName, err := r.agentNameForProfile(ctx, concrete)
	if err != nil {
		return ProfileExecution{}, err
	}
	return ProfileExecution{
		LogicalProfileID: profileID, ExecutionProfileID: decision.ExecutionProfileID,
		AgentName:      concreteAgentName,
		RouteSessionID: sessionID, Generation: decision.Generation,
		ProfileVersion: decision.ProfileVersion, Profile: concrete, Decision: decision,
	}, nil
}

// RouteAfterUnclassifiedFailure validates the logical profile and applies the
// narrow repeated-failure policy only with evidence built by a trusted task
// runtime boundary.
func (r *ProfileExecutionResolver) RouteAfterUnclassifiedFailure(
	ctx context.Context,
	sessionID, profileID, currentExecutionProfileID string,
	expectedGeneration int64,
	failure *routingerr.Error,
	evidence dynamic.UnclassifiedFailureEvidence,
) (dynamic.RouteDecision, error) {
	if r.engine == nil || sessionID == "" {
		return dynamic.RouteDecision{}, errors.New("dynamic profile execution is not configured")
	}
	if err := r.ValidateProfile(ctx, profileID); err != nil {
		return dynamic.RouteDecision{}, err
	}
	profile, err := r.loadDynamicProfileForSession(ctx, profileID, sessionID, "")
	if err != nil {
		return dynamic.RouteDecision{}, err
	}
	return r.engine.ApplyUnclassifiedFailureContext(
		ctx, sessionID, profile, expectedGeneration, currentExecutionProfileID, failure, evidence,
	)
}

// RouteAfterInterruptedFailure preserves the failed session's executor scope.
func (r *ProfileExecutionResolver) RouteAfterInterruptedFailure(ctx context.Context, sessionID, profileID, candidateID string, generation int64, failure *routingerr.Error) (dynamic.RouteDecision, error) {
	if r.engine == nil || sessionID == "" {
		return dynamic.RouteDecision{}, errors.New("dynamic profile execution is not configured")
	}
	if err := r.ValidateProfile(ctx, profileID); err != nil {
		return dynamic.RouteDecision{}, err
	}
	profile, err := r.loadDynamicProfileForSession(ctx, profileID, sessionID, "")
	if err != nil {
		return dynamic.RouteDecision{}, err
	}
	return r.engine.ApplyInterruptedFailureContext(ctx, sessionID, profile, generation, candidateID, failure)
}

// ClaimUnclassifiedFallbackLaunch performs the final contextual fence before
// a detached automatic successor begins launch work.
func (r *ProfileExecutionResolver) ClaimUnclassifiedFallbackLaunch(
	ctx context.Context,
	decision dynamic.RouteDecision,
	evidence dynamic.UnclassifiedFailureEvidence,
) error {
	if r == nil || r.engine == nil {
		return dynamic.ErrUnclassifiedWorkflowContextUnavailable
	}
	return r.engine.ClaimUnclassifiedFallbackLaunch(ctx, decision, evidence)
}

// ClearUnclassifiedStreak removes the session-owned count after a current
// successful output/effect/turn event has been validated by the orchestrator.
func (r *ProfileExecutionResolver) ClearUnclassifiedStreak(
	ctx context.Context,
	sessionID string,
	generation int64,
	candidateID string,
) error {
	if r == nil || r.engine == nil {
		return errors.New("dynamic profile execution is not configured")
	}
	return r.engine.ClearUnclassifiedStreak(ctx, sessionID, generation, candidateID)
}

// ClearUnclassifiedStartupStreak removes a startup count only after the
// lifecycle confirms that the agent session is initialized and ready.
func (r *ProfileExecutionResolver) ClearUnclassifiedStartupStreak(
	ctx context.Context,
	sessionID string,
	generation int64,
	candidateID string,
) error {
	if r == nil || r.engine == nil {
		return errors.New("dynamic profile execution is not configured")
	}
	return r.engine.ClearUnclassifiedStartupStreak(ctx, sessionID, generation, candidateID)
}

// ResolveExisting returns the persisted concrete execution for a logical
// session without advancing its route generation. It is used by resume paths
// after a restart, where selecting again would either fence a valid session
// or silently move it to another candidate.
func (r *ProfileExecutionResolver) ResolveExisting(
	ctx context.Context,
	sessionID, profileID, executionProfileID string,
	generation, profileVersion int64,
	reason string,
) (ProfileExecution, error) {
	if err := r.ValidateProfile(ctx, profileID); err != nil {
		return ProfileExecution{}, err
	}
	profile, err := r.profiles.GetAgentProfile(ctx, profileID)
	if err != nil {
		return ProfileExecution{}, err
	}
	agent, err := r.profiles.GetAgent(ctx, profile.AgentID)
	if err != nil {
		return ProfileExecution{}, err
	}
	if agent.Name != agents.DynamicAgentID {
		return ProfileExecution{LogicalProfileID: profileID, ExecutionProfileID: profileID, AgentName: agent.Name, Profile: profile}, nil
	}
	if executionProfileID == "" || generation <= 0 {
		return ProfileExecution{}, errors.New("dynamic session has no persisted execution profile")
	}
	concrete, err := r.profiles.GetAgentProfile(ctx, executionProfileID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve existing execution profile %s: %w", executionProfileID, err)
	}
	if concrete == nil || concrete.DeletedAt != nil || !concrete.Enabled {
		return ProfileExecution{}, fmt.Errorf("existing execution profile %s is unavailable", executionProfileID)
	}
	concreteAgentName, err := r.agentNameForProfile(ctx, concrete)
	if err != nil {
		return ProfileExecution{}, err
	}
	return ProfileExecution{
		LogicalProfileID: profileID, ExecutionProfileID: executionProfileID,
		AgentName:  concreteAgentName,
		Generation: generation, ProfileVersion: profileVersion, Profile: concrete,
		Decision: dynamic.RouteDecision{
			SessionID: sessionID, LogicalProfileID: profileID,
			ExecutionProfileID: executionProfileID, Generation: generation,
			ProfileVersion: profileVersion, Reason: reason,
		},
	}, nil
}

func (r *ProfileExecutionResolver) Resolve(ctx context.Context, sessionID, profileID string, expectedGeneration int64, excludeProfileID string) (ProfileExecution, error) {
	return r.resolve(ctx, sessionID, profileID, expectedGeneration, excludeProfileID, "")
}

// ResolveWithPreference keeps the current concrete candidate for an explicit
// retry when it remains eligible. Try-next callers continue to use Resolve and
// pass the current candidate as the one-time exclusion.
func (r *ProfileExecutionResolver) ResolveWithPreference(
	ctx context.Context,
	sessionID, profileID string,
	expectedGeneration int64,
	excludeProfileID, preferredProfileID string,
) (ProfileExecution, error) {
	return r.resolve(ctx, sessionID, profileID, expectedGeneration, excludeProfileID, preferredProfileID)
}

// ResolveRouteAction applies a manual route action to the durable route
// state. Retry resumes the current generation, even when its policy deadline
// has not elapsed; try-next claims a new generation and excludes the current
// candidate. The caller can then launch the returned concrete profile through
// the normal conductor path.
func (r *ProfileExecutionResolver) ResolveRouteAction(
	ctx context.Context,
	sessionID, profileID, currentExecutionProfileID string,
	expectedGeneration int64,
	action string,
) (ProfileExecution, error) {
	if r.profiles == nil {
		return ProfileExecution{}, errors.New("profile execution resolver has no profile store")
	}
	profile, err := r.profiles.GetAgentProfile(ctx, profileID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve profile %s: %w", profileID, err)
	}
	agent, err := r.profiles.GetAgent(ctx, profile.AgentID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve profile family %s: %w", profileID, err)
	}
	if agent.Name != agents.DynamicAgentID {
		return ProfileExecution{LogicalProfileID: profileID, ExecutionProfileID: profile.ID, AgentName: agent.Name, Profile: profile}, nil
	}
	if !r.enabled.Load() {
		return ProfileExecution{}, ErrDynamicRoutingDisabled
	}
	if r.dynamic == nil || r.engine == nil {
		return ProfileExecution{}, errors.New("dynamic profile execution is not configured")
	}
	if sessionID == "" {
		return ProfileExecution{}, errors.New("dynamic route action requires a session")
	}

	switch action {
	case "retry":
		return r.resolveRetryRouteAction(ctx, sessionID, profileID, currentExecutionProfileID, expectedGeneration)
	case "try_next", "skip":
		return r.resolveSkipRouteAction(ctx, sessionID, profileID, currentExecutionProfileID, expectedGeneration)
	case "cancel_wait", "stop":
		return r.resolveCancelRouteAction(ctx, sessionID, profileID, expectedGeneration, action)
	default:
		return ProfileExecution{}, fmt.Errorf("unsupported dynamic route action %q", action)
	}
}

// MarkRouteActive completes a claimed route's starting phase after the
// asynchronous agent process-start callback confirms success.
func (r *ProfileExecutionResolver) MarkRouteActive(ctx context.Context, sessionID string, expectedGeneration int64) error {
	if r.engine == nil {
		return errors.New("dynamic profile execution is not configured")
	}
	return r.engine.MarkActive(ctx, sessionID, expectedGeneration)
}

// MarkRouteActionRequired transitions a claimed route to durable
// action_required regardless of its current status. Callers use this so a
// launch failure after a generation claim always exposes a recovery action
// instead of leaving the route stuck at "starting".
func (r *ProfileExecutionResolver) MarkRouteActionRequired(
	ctx context.Context,
	sessionID string,
	expectedGeneration int64,
	reason string,
) (dynamic.RouteDecision, error) {
	if r.engine == nil {
		return dynamic.RouteDecision{}, errors.New("dynamic profile execution is not configured")
	}
	return r.engine.MarkActionRequired(ctx, sessionID, expectedGeneration, reason)
}

func (r *ProfileExecutionResolver) resolveRetryRouteAction(
	ctx context.Context,
	sessionID, profileID, currentExecutionProfileID string,
	expectedGeneration int64,
) (ProfileExecution, error) {
	state, exists, err := r.engine.LoadState(ctx, sessionID)
	if err != nil {
		return ProfileExecution{}, err
	}
	if exists && state.Generation != expectedGeneration {
		return ProfileExecution{}, dynamic.ErrStaleGeneration
	}
	if exists {
		if state.Status == dynamicRouteStatusRetrying {
			// A retry can survive a process restart without its in-memory owner.
			// Reclaim it only when this process does not own the launch.
			if r.engine.OwnsRetryClaim(sessionID, expectedGeneration) {
				return ProfileExecution{}, dynamic.ErrRecoveryPending
			}
			reclaimed, reclaimErr := r.engine.ReclaimRetrying(ctx, sessionID, expectedGeneration)
			if reclaimErr != nil {
				return ProfileExecution{}, reclaimErr
			}
			if reclaimed {
				return r.resolve(ctx, sessionID, profileID, expectedGeneration, "", currentExecutionProfileID)
			}
			return ProfileExecution{}, dynamic.ErrRecoveryPending
		}
		if state.Status == dynamicRouteStatusWaiting {
			// A route that exhausted every candidate has no pending candidate to
			// resume, so retrying it is a fresh selection with no preference. A
			// preference here would make the engine refuse every other candidate
			// while the preferred one stays suspended, so a session that waited
			// on one candidate could never reach a candidate that already freed up.
			return r.resolve(ctx, sessionID, profileID, expectedGeneration, "", "")
		}
		decision, resumeErr := r.engine.ResumePendingNow(ctx, sessionID, expectedGeneration)
		if resumeErr == nil {
			return r.executionFromDecisionWithRecovery(ctx, profileID, sessionID, decision)
		}
		if !errors.Is(resumeErr, dynamic.ErrRouteStateNotFound) {
			return ProfileExecution{}, resumeErr
		}
	}
	return r.resolve(ctx, sessionID, profileID, expectedGeneration, "", currentExecutionProfileID)
}

func (r *ProfileExecutionResolver) resolveSkipRouteAction(
	ctx context.Context,
	sessionID, profileID, currentExecutionProfileID string,
	expectedGeneration int64,
) (ProfileExecution, error) {
	if state, exists, err := r.engine.LoadState(ctx, sessionID); err != nil {
		return ProfileExecution{}, err
	} else if exists && state.Generation == expectedGeneration && state.Status == dynamicRouteStatusRetrying {
		if r.engine.OwnsRetryClaim(sessionID, expectedGeneration) {
			return ProfileExecution{}, dynamic.ErrRecoveryPending
		}
		reclaimed, reclaimErr := r.engine.ReclaimRetrying(ctx, sessionID, expectedGeneration)
		if reclaimErr != nil {
			return ProfileExecution{}, reclaimErr
		}
		if !reclaimed {
			return ProfileExecution{}, dynamic.ErrRecoveryPending
		}
	}
	profileConfig, err := r.loadDynamicProfileForSession(ctx, profileID, sessionID, "")
	if err != nil {
		return ProfileExecution{}, err
	}
	if currentExecutionProfileID == "" {
		state, exists, stateErr := r.engine.LoadState(ctx, sessionID)
		if stateErr != nil {
			return ProfileExecution{}, stateErr
		}
		if exists {
			currentExecutionProfileID = state.ExecutionProfileID
		}
	}
	decision, err := r.engine.SelectContextWithReason(
		ctx, sessionID, profileConfig, expectedGeneration, currentExecutionProfileID, "manual_skip",
	)
	if err != nil {
		return ProfileExecution{}, err
	}
	return r.executionFromDecision(ctx, profileID, sessionID, decision)
}

func (r *ProfileExecutionResolver) resolveCancelRouteAction(
	ctx context.Context,
	sessionID, profileID string,
	expectedGeneration int64,
	action string,
) (ProfileExecution, error) {
	reason := "manual_cancel_wait"
	if action == "stop" {
		reason = "manual_stop"
	}
	state, exists, err := r.engine.LoadState(ctx, sessionID)
	if err != nil {
		return ProfileExecution{}, err
	}
	if exists && state.Generation == expectedGeneration && state.Status == dynamicRouteStatusRetrying {
		if r.engine.OwnsRetryClaim(sessionID, expectedGeneration) {
			return ProfileExecution{}, dynamic.ErrRecoveryPending
		}
		reclaimed, reclaimErr := r.engine.ReclaimRetrying(ctx, sessionID, expectedGeneration)
		if reclaimErr != nil {
			return ProfileExecution{}, reclaimErr
		}
		if !reclaimed {
			return ProfileExecution{}, dynamic.ErrRecoveryPending
		}
	}
	decision, err := r.engine.CancelPending(ctx, sessionID, expectedGeneration, reason)
	if err != nil {
		return ProfileExecution{}, err
	}
	return r.executionFromDecision(ctx, profileID, sessionID, decision)
}

// ResumePendingRoute advances a due durable policy wait/retry without
// selecting another generation. It is used by the orchestrator's recovery
// scheduler after the persisted deadline has elapsed.
func (r *ProfileExecutionResolver) ResumePendingRoute(
	ctx context.Context,
	sessionID string,
	expectedGeneration int64,
) (ProfileExecution, error) {
	if r.engine == nil || r.profiles == nil {
		return ProfileExecution{}, errors.New("dynamic profile execution is not configured")
	}
	if !r.enabled.Load() {
		return ProfileExecution{}, ErrDynamicRoutingDisabled
	}
	state, exists, err := r.engine.LoadState(ctx, sessionID)
	if err != nil {
		return ProfileExecution{}, err
	}
	if !exists {
		return ProfileExecution{}, dynamic.ErrRouteStateNotFound
	}
	decision, err := r.engine.ResumePending(ctx, sessionID, expectedGeneration)
	if err != nil {
		return ProfileExecution{}, err
	}
	return r.executionFromDecisionWithRecovery(ctx, state.LogicalProfileID, sessionID, decision)
}

// MarkRouteRecoveryActionRequired returns a claimed "retrying" route to
// manual recovery after its resumed launch failed.
func (r *ProfileExecutionResolver) MarkRouteRecoveryActionRequired(
	ctx context.Context,
	sessionID string,
	expectedGeneration int64,
) error {
	if r.engine == nil {
		return nil
	}
	return r.engine.MarkRecoveryActionRequired(ctx, sessionID, expectedGeneration)
}

func (r *ProfileExecutionResolver) executionFromDecisionWithRecovery(
	ctx context.Context,
	profileID, sessionID string,
	decision dynamic.RouteDecision,
) (ProfileExecution, error) {
	execution, err := r.executionFromDecision(ctx, profileID, sessionID, decision)
	if err == nil || decision.Status != dynamicRouteStatusRetrying {
		return execution, err
	}
	if recoveryErr := r.MarkRouteRecoveryActionRequired(ctx, sessionID, decision.Generation); recoveryErr != nil {
		return ProfileExecution{}, fmt.Errorf("%w; restore route recovery: %v", err, recoveryErr)
	}
	return ProfileExecution{}, fmt.Errorf("%w: %v", dynamic.ErrRecoveryPending, err)
}

func (r *ProfileExecutionResolver) resolve(
	ctx context.Context,
	sessionID, profileID string,
	expectedGeneration int64,
	excludeProfileID, preferredProfileID string,
) (ProfileExecution, error) {
	if r.profiles == nil {
		return ProfileExecution{}, errors.New("profile execution resolver has no profile store")
	}
	profile, err := r.profiles.GetAgentProfile(ctx, profileID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve profile %s: %w", profileID, err)
	}
	agent, err := r.profiles.GetAgent(ctx, profile.AgentID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve profile family %s: %w", profileID, err)
	}
	if agent.Name != agents.DynamicAgentID {
		return ProfileExecution{
			LogicalProfileID: profileID, ExecutionProfileID: profileID, Profile: profile,
		}, nil
	}
	if !r.enabled.Load() {
		return ProfileExecution{}, ErrDynamicRoutingDisabled
	}
	if r.dynamic == nil || r.engine == nil {
		return ProfileExecution{}, errors.New("dynamic profile execution is not configured")
	}
	profileConfig, err := r.loadDynamicProfileForSession(ctx, profileID, sessionID, "")
	if err != nil {
		return ProfileExecution{}, err
	}
	decision, err := r.engine.SelectContextWithPreference(
		ctx, sessionID, profileConfig, expectedGeneration, excludeProfileID, preferredProfileID,
	)
	if err != nil {
		return ProfileExecution{}, err
	}
	concrete, err := r.profiles.GetAgentProfile(ctx, decision.ExecutionProfileID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve execution profile %s: %w", decision.ExecutionProfileID, err)
	}
	concreteAgentName, err := r.agentNameForProfile(ctx, concrete)
	if err != nil {
		return ProfileExecution{}, err
	}
	return ProfileExecution{
		LogicalProfileID: profileID, ExecutionProfileID: decision.ExecutionProfileID,
		AgentName:      concreteAgentName,
		RouteSessionID: sessionID,
		Generation:     decision.Generation, ProfileVersion: decision.ProfileVersion,
		Profile:  concrete,
		Decision: decision,
	}, nil
}

func (r *ProfileExecutionResolver) executionFromDecision(
	ctx context.Context,
	profileID, sessionID string,
	decision dynamic.RouteDecision,
) (ProfileExecution, error) {
	concrete, err := r.profiles.GetAgentProfile(ctx, decision.ExecutionProfileID)
	if err != nil {
		return ProfileExecution{}, fmt.Errorf("resolve execution profile %s: %w", decision.ExecutionProfileID, err)
	}
	if concrete == nil || concrete.DeletedAt != nil || !concrete.Enabled {
		return ProfileExecution{}, fmt.Errorf("execution profile %s is unavailable", decision.ExecutionProfileID)
	}
	concreteAgentName, err := r.agentNameForProfile(ctx, concrete)
	if err != nil {
		return ProfileExecution{}, err
	}
	return ProfileExecution{
		LogicalProfileID: profileID, ExecutionProfileID: decision.ExecutionProfileID,
		AgentName:      concreteAgentName,
		RouteSessionID: sessionID, Generation: decision.Generation,
		ProfileVersion: decision.ProfileVersion, Profile: concrete, Decision: decision,
	}, nil
}

// dynamicSourceProfileID returns the profile whose dynamic candidate set backs
// logicalProfileID. An Office identifier binds an execution profile (spec
// dynamic-agent-routing "Use in Office"): the bound profile owns the routes
// while the Office ID remains the logical session identity. Ordinary profiles
// keep selecting themselves.
func (r *ProfileExecutionResolver) dynamicSourceProfileID(ctx context.Context, logicalProfileID string) (string, error) {
	if r.profiles == nil {
		return logicalProfileID, nil
	}
	profile, err := r.profiles.GetAgentProfile(ctx, logicalProfileID)
	if err != nil {
		return "", fmt.Errorf("resolve profile %s: %w", logicalProfileID, err)
	}
	sourceProfileID := logicalProfileID
	if profile != nil && profile.ExecutionAgentProfileID != "" {
		sourceProfileID = profile.ExecutionAgentProfileID
	}
	if err := r.validateDynamicSourceProfile(ctx, logicalProfileID, sourceProfileID, profile); err != nil {
		return "", err
	}
	return sourceProfileID, nil
}

func (r *ProfileExecutionResolver) validateDynamicSourceProfile(
	ctx context.Context, logicalProfileID, sourceProfileID string, logicalProfile *agentsettingsmodels.AgentProfile,
) error {
	source, err := r.profiles.GetAgentProfile(ctx, sourceProfileID)
	if err != nil {
		return fmt.Errorf("resolve dynamic source profile %s: %w", sourceProfileID, err)
	}
	if source == nil || source.DeletedAt != nil || !source.Enabled {
		return fmt.Errorf("dynamic source profile %s is unavailable", sourceProfileID)
	}
	if logicalProfile != nil && sourceProfileID != logicalProfileID &&
		source.WorkspaceID != "" && source.WorkspaceID != logicalProfile.WorkspaceID {
		return fmt.Errorf("dynamic source profile %s belongs to a different workspace", sourceProfileID)
	}
	agent, err := r.profiles.GetAgent(ctx, source.AgentID)
	if err != nil {
		return fmt.Errorf("resolve dynamic source profile family %s: %w", sourceProfileID, err)
	}
	if agent == nil || agent.Name != agents.DynamicAgentID {
		return fmt.Errorf("execution profile %s is not a dynamic profile", sourceProfileID)
	}
	return nil
}

func (r *ProfileExecutionResolver) agentNameForProfile(
	ctx context.Context,
	profile *agentsettingsmodels.AgentProfile,
) (string, error) {
	if profile == nil {
		return "", errors.New("cannot resolve agent name for a nil profile")
	}
	if profile.AgentID == "" {
		return "", nil
	}
	agent, err := r.profiles.GetAgent(ctx, profile.AgentID)
	if err != nil {
		return "", fmt.Errorf("resolve agent for execution profile %s: %w", profile.ID, err)
	}
	return agent.Name, nil
}

// SessionExecutorResolver reports the executor a session runs on. The executor
// lives on the task session rather than the agent profile, so it is the only
// authoritative source for whether a candidate's agent authenticates against the
// backend host's provider account.
//
// A resolver that returns an empty executor keeps the host default, which is
// what an unqualified session launches on.
type SessionExecutorResolver func(ctx context.Context, sessionID string) (string, error)

// SetSessionExecutorResolver injects the session-executor lookup. With it, every
// selection entry point carries the execution environment into usage
// attribution, instead of only the paths that thread an executor explicitly.
func (r *ProfileExecutionResolver) SetSessionExecutorResolver(
	resolve SessionExecutorResolver,
) {
	r.sessionExecutors = resolve
}

// sessionExecutor resolves a session's executor, degrading to empty when the
// lookup is unavailable or fails. Degrading to the host default rather than
// remote keeps the established behaviour for callers that cannot be classified,
// while an executor that is positively known to be remote is never lost.
func (r *ProfileExecutionResolver) sessionExecutor(ctx context.Context, sessionID string) string {
	if r == nil || r.sessionExecutors == nil || sessionID == "" {
		return ""
	}
	executorID, err := r.sessionExecutors(ctx, sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(executorID)
}

func (r *ProfileExecutionResolver) loadDynamicProfile(ctx context.Context, profileID string) (dynamic.Profile, error) {
	return r.loadDynamicProfileForExecutor(ctx, profileID, "")
}

// loadDynamicProfileForSession resolves the execution environment from an
// explicit executor when the caller has one, and otherwise from the session. An
// explicit value wins so a caller that already knows the executor is never
// second-guessed by a lookup.
func (r *ProfileExecutionResolver) loadDynamicProfileForSession(
	ctx context.Context,
	profileID string,
	sessionID string,
	executorID string,
) (dynamic.Profile, error) {
	if strings.TrimSpace(executorID) == "" {
		executorID = r.sessionExecutor(ctx, sessionID)
	}
	return r.loadDynamicProfileForExecutor(ctx, profileID, executorID)
}

// loadDynamicProfileForExecutor loads the profile and marks every candidate's
// execution environment. Passing the session's executor ID is what lets usage
// attribution tell a host execution from a remote one; omitting it keeps the
// host default, which is what an unqualified profile launches on.
func (r *ProfileExecutionResolver) loadDynamicProfileForExecutor(
	ctx context.Context,
	profileID string,
	executorID string,
) (dynamic.Profile, error) {
	if !r.enabled.Load() {
		return dynamic.Profile{}, ErrDynamicRoutingDisabled
	}
	if r.dynamic == nil {
		return dynamic.Profile{}, errors.New("dynamic profile execution is not configured")
	}
	sourceProfileID, err := r.dynamicSourceProfileID(ctx, profileID)
	if err != nil {
		return dynamic.Profile{}, err
	}
	config, routes, err := r.dynamic.GetDynamicAgentProfile(ctx, sourceProfileID)
	if err != nil {
		return dynamic.Profile{}, fmt.Errorf("load dynamic profile %s: %w", profileID, err)
	}
	profile := dynamic.Profile{
		ID: profileID, Version: config.Version,
		KeepModelWhileRunning: config.KeepModelWhileRunning,
		Candidates:            make([]dynamic.Candidate, 0, len(routes)),
	}
	remoteExecution := executionEnvironment(executorID) != "local"
	for _, route := range routes {
		candidate, err := r.resolveDynamicCandidate(ctx, route, remoteExecution)
		if err != nil {
			return dynamic.Profile{}, err
		}
		profile.Candidates = append(profile.Candidates, candidate)
	}
	r.applyResourceLimits(ctx, profile.Candidates)
	return profile, nil
}

// resolveDynamicCandidate turns one saved route into a runtime candidate. A
// deleted or disabled concrete profile leaves the row present but disabled, so
// the tier layout and its numbering survive an operator removing a model.
func (r *ProfileExecutionResolver) resolveDynamicCandidate(
	ctx context.Context,
	route agentsettingsmodels.DynamicAgentRoute,
	remoteExecution bool,
) (dynamic.Candidate, error) {
	candidate := dynamic.Candidate{
		ID: route.ExecutionProfileID, Enabled: route.Enabled,
		BindingKey:      dynamic.ResourceKey(dynamic.ScopeProfile, route.ExecutionProfileID),
		RemoteExecution: remoteExecution,
	}
	if route.RulesJSON != "" {
		policy, legacyRules, selection, err := decodeDynamicRoutePolicy(route.RulesJSON)
		if err != nil {
			return dynamic.Candidate{}, fmt.Errorf(
				"decode dynamic route %s: %w", route.ExecutionProfileID, err)
		}
		candidate.Policies = policy
		candidate.Rules = legacyRules
		candidate.Selection = selection
	}
	concrete, err := r.profiles.GetAgentProfile(ctx, route.ExecutionProfileID)
	switch {
	case err != nil:
		if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, store.ErrAgentProfileDeleted) {
			return dynamic.Candidate{}, fmt.Errorf(
				"load dynamic candidate %s: %w", route.ExecutionProfileID, err)
		}
		candidate.Enabled = false
	case concrete == nil || concrete.DeletedAt != nil || !concrete.Enabled:
		candidate.Enabled = false
	default:
		// The launched model identifies which provider windows belong to this
		// candidate, so a window scoped to another model cannot be its usage.
		candidate.ModelID = strings.TrimSpace(concrete.Model)
	}
	if candidate.Enabled && r.bindingResolver != nil {
		candidate.BindingKey = dynamic.ResourceKey(
			dynamic.ScopeCredential,
			r.bindingResolver.Resolve(
				profileCredentialBindingDescriptor(concrete), route.ExecutionProfileID,
			),
		)
	}
	return candidate, nil
}

// decodeDynamicRoutePolicy splits the stored document into the failure-policy
// half consumed by routingpolicy and the additive selection half consumed by
// tier routing. Failure-policy evaluation never sees selection, and an absent
// selection yields the legacy ordered defaults.
func decodeDynamicRoutePolicy(raw string) (routingpolicy.Document, map[string]dynamic.Action, dynamic.Selection, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return routingpolicy.Document{}, nil, dynamic.Selection{}, err
	}
	if _, hasVersion := fields["version"]; hasVersion {
		var document routingpolicy.Document
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			return routingpolicy.Document{}, nil, dynamic.Selection{}, err
		}
		if err := routingpolicy.ValidateDocument(document); err != nil {
			return routingpolicy.Document{}, nil, dynamic.Selection{}, err
		}
		return document, nil, decodeDynamicRouteSelection(fields), nil
	}
	var legacy map[string]dynamic.Action
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		return routingpolicy.Document{}, nil, dynamic.Selection{}, err
	}
	document := routingpolicy.DefaultDocument()
	classActions := make(map[routingerr.Class]dynamic.Action)
	for key, action := range legacy {
		if key == "on_provider_error" {
			document.Transient = legacyActionPolicy(action)
			document.Hard = legacyActionPolicy(action)
			continue
		}
		class := routingerr.ClassForCode(routingerr.Code(key))
		if class != routingerr.ClassTransient && class != routingerr.ClassHard {
			return routingpolicy.Document{}, nil, dynamic.Selection{}, fmt.Errorf("legacy rule %q is not a provider error code", key)
		}
		if previous, ok := classActions[class]; ok && previous != action {
			return routingpolicy.Document{}, nil, dynamic.Selection{}, fmt.Errorf("legacy rules conflict for %s errors", class)
		}
		classActions[class] = action
		if class == routingerr.ClassTransient {
			document.Transient = legacyActionPolicy(action)
		} else {
			document.Hard = legacyActionPolicy(action)
		}
	}
	if err := routingpolicy.ValidateDocument(document); err != nil {
		return routingpolicy.Document{}, nil, dynamic.Selection{}, err
	}
	return document, legacy, dynamic.Selection{}, nil
}

// dynamicRouteSelectionDocument is the additive selection subobject. A missing
// or empty document is the legacy ordered configuration, not an error.
type dynamicRouteSelectionDocument struct {
	JoinPrevious bool                       `json:"join_previous"`
	Tier         *dynamicRouteTierDocument  `json:"tier"`
	Model        *dynamicRouteModelDocument `json:"model"`
}

type dynamicRouteTierDocument struct {
	Mode      string `json:"mode"`
	OnFailure string `json:"on_failure"`
}

type dynamicRouteModelDocument struct {
	Cost                 string                    `json:"cost"`
	UsageSource          string                    `json:"usage_source"`
	ReservedUserSharePct int                       `json:"reserved_user_share_pct"`
	Windows              []dynamicRouteUsageWindow `json:"windows"`
}

type dynamicRouteUsageWindow struct {
	Period string                `json:"period"`
	Unit   string                `json:"unit"`
	Limit  string                `json:"limit"`
	Reset  *dynamicRouteResetDoc `json:"reset"`
	Scope  string                `json:"scope"`
}

type dynamicRouteResetDoc struct {
	Anchor   string `json:"anchor"`
	Timezone string `json:"timezone"`
}

func decodeDynamicRouteSelection(fields map[string]json.RawMessage) dynamic.Selection {
	raw, ok := fields["selection"]
	if !ok {
		return dynamic.Selection{}
	}
	var document dynamicRouteSelectionDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return dynamic.Selection{}
	}
	return document.toSelection()
}

func (d dynamicRouteSelectionDocument) toSelection() dynamic.Selection {
	selection := dynamic.Selection{JoinPrevious: d.JoinPrevious}
	if d.Tier != nil {
		selection.Tier = &dynamic.TierPolicy{
			Mode:      dynamic.TierMode(d.Tier.Mode),
			OnFailure: dynamic.FailureDirection(d.Tier.OnFailure),
		}
	}
	if d.Model != nil {
		selection.Model = d.Model.toModelOptions()
	}
	return selection
}

func (d dynamicRouteModelDocument) toModelOptions() dynamic.ModelOptions {
	options := dynamic.ModelOptions{
		Cost:                 dynamic.CostClass(d.Cost),
		UsageSource:          dynamic.UsageSource(d.UsageSource),
		ReservedUserSharePct: d.ReservedUserSharePct,
	}
	for _, window := range d.Windows {
		converted := dynamic.UsageWindow{
			Period: window.Period, Unit: window.Unit, Limit: window.Limit, Scope: window.Scope,
		}
		if window.Reset != nil {
			converted.ResetAnchor = window.Reset.Anchor
			converted.Timezone = window.Reset.Timezone
		}
		options.Windows = append(options.Windows, converted)
	}
	return options
}

func legacyActionPolicy(action dynamic.Action) routingpolicy.Policy {
	policy := routingpolicy.DefaultPolicy()
	switch action {
	case dynamic.ActionRetrySame:
		policy.Retry = routingpolicy.RetryPolicy{Enabled: true, MaxRetries: 1, InitialIntervalSeconds: 5}
		policy.OnExhausted = routingpolicy.OutcomeStop
	case dynamic.ActionStop:
		policy.OnExhausted = routingpolicy.OutcomeStop
	}
	return policy
}

func profileCredentialBindingDescriptor(profile *agentsettingsmodels.AgentProfile) dynamic.CredentialBindingDescriptor {
	if profile == nil {
		return dynamic.CredentialBindingDescriptor{}
	}
	descriptor := dynamic.CredentialBindingDescriptor{
		Version:              1,
		AgentFamilyID:        profile.AgentID,
		AuthenticationMethod: strings.TrimSpace(profile.BillingType),
		ExecutorNamespace:    "local",
		AuthorizationScope:   "agent_runtime",
	}
	secretIDs := make([]string, 0, len(profile.EnvVars))
	for _, envVar := range profile.EnvVars {
		if strings.TrimSpace(envVar.SecretID) != "" {
			secretIDs = append(secretIDs, strings.TrimSpace(envVar.SecretID))
		}
	}
	if len(secretIDs) > 0 {
		sort.Strings(secretIDs)
		descriptor.CredentialSourceKind = "profile_secret"
		descriptor.CredentialLocator = strings.Join(secretIDs, ",")
	} else if descriptor.AuthenticationMethod != "" {
		descriptor.CredentialSourceKind = "agent_credentials"
		descriptor.CredentialLocator = profile.AgentID + ":" + descriptor.AuthenticationMethod
	}
	return descriptor
}
