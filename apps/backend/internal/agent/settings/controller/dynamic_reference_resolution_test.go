package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/settings/dto"
	"github.com/kandev/kandev/internal/agent/settings/models"
	"github.com/kandev/kandev/internal/agent/settings/store"
)

func resolutionPolicy(join bool, tier *dto.DynamicAgentTierPolicyDTO) *dto.DynamicAgentPolicyDTO {
	return &dto.DynamicAgentPolicyDTO{
		Version:   1,
		Transient: dto.DynamicErrorPolicyDTO{OnExhausted: "skip"},
		Hard:      dto.DynamicErrorPolicyDTO{OnExhausted: "skip"},
		Selection: &dto.DynamicAgentSelectionDTO{JoinPrevious: join, Tier: tier},
	}
}

func resolutionRoutes(t *testing.T, candidates []dto.DynamicAgentCandidateDTO) []models.DynamicAgentRoute {
	t.Helper()
	profile := &dto.DynamicAgentProfileDTO{Candidates: candidates}
	routes, err := dynamicRoutesFromDTO("dynamic", profile)
	if err != nil {
		t.Fatalf("dynamicRoutesFromDTO: %v", err)
	}
	return routes
}

func routePolicy(t *testing.T, route models.DynamicAgentRoute) dto.DynamicAgentPolicyDTO {
	t.Helper()
	policy, err := decodeDynamicPolicyDocument(route.RulesJSON, route.Position)
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	return policy
}

func candidateIDs(routes []models.DynamicAgentRoute) []string {
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		ids = append(ids, route.ExecutionProfileID)
	}
	return ids
}

// TestRemoveDynamicCandidateTierHeadInheritance pins AC-.4: removing a tier
// head promotes the next same-tier row to head with the removed head's
// mode/on_failure, renumbers positions, and never merges or reorders tiers.
func TestRemoveDynamicCandidateTierHeadInheritance(t *testing.T) {
	pace := &dto.DynamicAgentTierPolicyDTO{Mode: "pace", OnFailure: "next_tier"}
	order := &dto.DynamicAgentTierPolicyDTO{Mode: "order", OnFailure: "same_tier_next"}

	t.Run("head removal promotes joined follower", func(t *testing.T) {
		routes := resolutionRoutes(t, []dto.DynamicAgentCandidateDTO{
			{Position: 0, ExecutionProfileID: "a", Enabled: true, Policies: resolutionPolicy(false, pace)},
			{Position: 1, ExecutionProfileID: "b", Enabled: true, Policies: resolutionPolicy(true, nil)},
			{Position: 2, ExecutionProfileID: "c", Enabled: true, Policies: resolutionPolicy(false, order)},
		})
		got, changed, err := removeDynamicCandidate(routes, "a")
		if err != nil || !changed {
			t.Fatalf("removeDynamicCandidate changed=%v err=%v", changed, err)
		}
		if want := []string{"b", "c"}; !equalStrings(candidateIDs(got), want) {
			t.Fatalf("routes = %v, want %v", candidateIDs(got), want)
		}
		if got[0].Position != 0 || got[1].Position != 1 {
			t.Fatalf("positions = %d,%d, want 0,1", got[0].Position, got[1].Position)
		}
		promoted := routePolicy(t, got[0])
		if promoted.Selection == nil || promoted.Selection.JoinPrevious {
			t.Fatalf("promoted follower must stop joining: %#v", promoted.Selection)
		}
		if promoted.Selection.Tier == nil || promoted.Selection.Tier.Mode != "pace" || promoted.Selection.Tier.OnFailure != "next_tier" {
			t.Fatalf("promoted follower tier = %#v, want inherited pace/next_tier", promoted.Selection.Tier)
		}
	})

	t.Run("non-head removal leaves tiers untouched", func(t *testing.T) {
		routes := resolutionRoutes(t, []dto.DynamicAgentCandidateDTO{
			{Position: 0, ExecutionProfileID: "a", Enabled: true, Policies: resolutionPolicy(false, pace)},
			{Position: 1, ExecutionProfileID: "b", Enabled: true, Policies: resolutionPolicy(true, nil)},
		})
		got, changed, err := removeDynamicCandidate(routes, "b")
		if err != nil || !changed {
			t.Fatalf("removeDynamicCandidate changed=%v err=%v", changed, err)
		}
		if want := []string{"a"}; !equalStrings(candidateIDs(got), want) {
			t.Fatalf("routes = %v, want %v", candidateIDs(got), want)
		}
	})

	t.Run("head removal with a new head next does not inherit", func(t *testing.T) {
		routes := resolutionRoutes(t, []dto.DynamicAgentCandidateDTO{
			{Position: 0, ExecutionProfileID: "a", Enabled: true, Policies: resolutionPolicy(false, pace)},
			{Position: 1, ExecutionProfileID: "b", Enabled: true, Policies: resolutionPolicy(false, order)},
		})
		got, changed, err := removeDynamicCandidate(routes, "a")
		if err != nil || !changed {
			t.Fatalf("removeDynamicCandidate changed=%v err=%v", changed, err)
		}
		kept := routePolicy(t, got[0])
		if kept.Selection.Tier == nil || kept.Selection.Tier.Mode != "order" {
			t.Fatalf("next head tier = %#v, want its own order tier", kept.Selection.Tier)
		}
	})

	t.Run("sole row removal yields no rows", func(t *testing.T) {
		routes := resolutionRoutes(t, []dto.DynamicAgentCandidateDTO{
			{Position: 0, ExecutionProfileID: "a", Enabled: true, Policies: resolutionPolicy(false, pace)},
		})
		got, changed, err := removeDynamicCandidate(routes, "a")
		if err != nil || !changed || len(got) != 0 {
			t.Fatalf("removeDynamicCandidate got=%v changed=%v err=%v", got, changed, err)
		}
	})
}

// TestResolveDynamicCandidateRoutesRejectsEmptyRemoval pins the builder guard:
// removing the only candidate row would leave an invalid empty document, so it
// fails closed, while disabling the same sole row stays valid.
func TestResolveDynamicCandidateRoutesRejectsEmptyRemoval(t *testing.T) {
	pace := &dto.DynamicAgentTierPolicyDTO{Mode: "pace", OnFailure: "next_tier"}
	routes := resolutionRoutes(t, []dto.DynamicAgentCandidateDTO{
		{Position: 0, ExecutionProfileID: "a", Enabled: true, Policies: resolutionPolicy(false, pace)},
	})
	if _, _, err := resolveDynamicCandidateRoutes(routes, "a", dynamicResolutionActionRemove); !errors.Is(err, ErrDynamicProfileWouldEmpty) {
		t.Fatalf("remove sole candidate err = %v, want ErrDynamicProfileWouldEmpty", err)
	}
	if _, matched, err := resolveDynamicCandidateRoutes(routes, "a", dynamicResolutionActionDisable); err != nil || !matched {
		t.Fatalf("disable sole candidate err = %v matched = %v, want no error and matched", err, matched)
	}
}

func TestDisableDynamicCandidateClearsEnabled(t *testing.T) {
	order := &dto.DynamicAgentTierPolicyDTO{Mode: "order", OnFailure: "same_tier_next"}
	routes := resolutionRoutes(t, []dto.DynamicAgentCandidateDTO{
		{Position: 0, ExecutionProfileID: "a", Enabled: true, Policies: resolutionPolicy(false, order)},
		{Position: 1, ExecutionProfileID: "b", Enabled: true, Policies: resolutionPolicy(false, order)},
	})
	got, matched := disableDynamicCandidate(routes, "a")
	if !matched {
		t.Fatal("disableDynamicCandidate must report the candidate as present")
	}
	if got[0].Enabled {
		t.Fatal("candidate row must be disabled")
	}
	if !got[1].Enabled {
		t.Fatal("unrelated row must keep its enabled state")
	}
	if _, matchedMissing := disableDynamicCandidate(routes, "absent"); matchedMissing {
		t.Fatal("disableDynamicCandidate must report an absent candidate as unmatched")
	}
}

func TestValidateDynamicResolutionRequestRejectsDeleteDisable(t *testing.T) {
	err := validateDynamicResolutionRequest(DynamicReferenceResolutionRequest{
		ProfileID: "p", Operation: "delete", Action: "disable_candidate",
		Versions: map[string]int64{"d": 1},
	})
	if !errors.Is(err, ErrDynamicResolutionInvalid) {
		t.Fatalf("err = %v, want ErrDynamicResolutionInvalid", err)
	}
}

func TestResolveDynamicReferencesDisablesCandidateAndProfile(t *testing.T) {
	ctrl, repo, fixture := newResolutionFixture(t)
	dynamicRepo := repo.(store.DynamicProfileRepository)

	refs, err := ctrl.listDynamicProfileReferences(context.Background(), fixture.candidateID)
	if err != nil {
		t.Fatalf("list references: %v", err)
	}
	if len(refs) != 1 || refs[0].Version != 1 || len(refs[0].Routes) != 1 || refs[0].Routes[0].Tier != 1 {
		t.Fatalf("references = %#v, want one route at tier 1 version 1", refs)
	}

	result, err := ctrl.ResolveDynamicReferences(context.Background(), DynamicReferenceResolutionRequest{
		ProfileID: fixture.candidateID,
		Operation: "disable",
		Action:    "disable_candidate",
		Versions:  map[string]int64{fixture.dynamicID: refs[0].Version},
	})
	if err != nil {
		t.Fatalf("ResolveDynamicReferences: %v", err)
	}
	if result.Enabled {
		t.Fatal("target profile must be disabled")
	}
	stored, routes, err := dynamicRepo.GetDynamicAgentProfile(context.Background(), fixture.dynamicID)
	if err != nil {
		t.Fatalf("get dynamic profile: %v", err)
	}
	if stored.Version != 2 {
		t.Fatalf("dynamic version = %d, want 2", stored.Version)
	}
	if len(routes) != 2 || routes[0].ExecutionProfileID != fixture.candidateID || routes[0].Enabled {
		t.Fatalf("routes = %#v, want candidate kept disabled", routes)
	}
}

func TestResolveDynamicReferencesRemovePromotesTierHead(t *testing.T) {
	ctrl, repo, fixture := newResolutionFixture(t)
	dynamicRepo := repo.(store.DynamicProfileRepository)

	refs, err := ctrl.listDynamicProfileReferences(context.Background(), fixture.candidateID)
	if err != nil {
		t.Fatalf("list references: %v", err)
	}
	if _, err := ctrl.ResolveDynamicReferences(context.Background(), DynamicReferenceResolutionRequest{
		ProfileID: fixture.candidateID,
		Operation: "delete",
		Action:    "remove_candidate",
		Versions:  map[string]int64{fixture.dynamicID: refs[0].Version},
	}); err != nil {
		t.Fatalf("ResolveDynamicReferences: %v", err)
	}
	_, routes, err := dynamicRepo.GetDynamicAgentProfile(context.Background(), fixture.dynamicID)
	if err != nil {
		t.Fatalf("get dynamic profile: %v", err)
	}
	if len(routes) != 1 || routes[0].ExecutionProfileID != fixture.otherID {
		t.Fatalf("routes = %#v, want only the other candidate", routes)
	}
	promoted := routePolicy(t, routes[0])
	if promoted.Selection == nil || promoted.Selection.JoinPrevious || promoted.Selection.Tier == nil || promoted.Selection.Tier.Mode != "pace" {
		t.Fatalf("promoted tier = %#v, want inherited pace head", promoted.Selection)
	}
	if _, err := repo.GetAgentProfile(context.Background(), fixture.candidateID); err == nil {
		t.Fatal("target profile must be deleted")
	}
	deleted, err := repo.GetAgentProfileIncludingDeleted(context.Background(), fixture.candidateID)
	if err != nil || deleted.DeletedAt == nil {
		t.Fatalf("target row = %#v err = %v, want soft-deleted", deleted, err)
	}
}

func TestResolveDynamicReferencesStaleVersionChangesNothing(t *testing.T) {
	ctrl, repo, fixture := newResolutionFixture(t)
	dynamicRepo := repo.(store.DynamicProfileRepository)

	_, err := ctrl.ResolveDynamicReferences(context.Background(), DynamicReferenceResolutionRequest{
		ProfileID: fixture.candidateID,
		Operation: "disable",
		Action:    "disable_candidate",
		Versions:  map[string]int64{fixture.dynamicID: 99},
	})
	if !errors.Is(err, ErrDynamicProfileVersionConflict) {
		t.Fatalf("err = %v, want ErrDynamicProfileVersionConflict", err)
	}
	stored, routes, err := dynamicRepo.GetDynamicAgentProfile(context.Background(), fixture.dynamicID)
	if err != nil {
		t.Fatalf("get dynamic profile: %v", err)
	}
	if stored.Version != 1 || len(routes) != 2 || !routes[0].Enabled {
		t.Fatalf("dynamic profile changed after conflict: version=%d routes=%#v", stored.Version, routes)
	}
	profile, err := repo.GetAgentProfile(context.Background(), fixture.candidateID)
	if err != nil || !profile.Enabled {
		t.Fatalf("target profile = %#v err = %v, want unchanged enabled", profile, err)
	}
}

// TestResolveDynamicReferencesRemoveSoleCandidateRejectsAndChangesNothing
// confirms the zero-candidate case fails before any write: the whole request is
// rejected with the named error (carrying the blocking profile name), the target
// profile stays enabled, and the dynamic document is byte-for-byte unchanged.
func TestResolveDynamicReferencesRemoveSoleCandidateRejectsAndChangesNothing(t *testing.T) {
	ctrl, repo, fixture := newSoleCandidateResolutionFixture(t)
	dynamicRepo := repo.(store.DynamicProfileRepository)

	refs, err := ctrl.listDynamicProfileReferences(context.Background(), fixture.candidateID)
	if err != nil {
		t.Fatalf("list references: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("references = %#v, want exactly one", refs)
	}
	_, err = ctrl.ResolveDynamicReferences(context.Background(), DynamicReferenceResolutionRequest{
		ProfileID: fixture.candidateID,
		Operation: "delete",
		Action:    "remove_candidate",
		Versions:  map[string]int64{fixture.dynamicID: refs[0].Version},
	})
	if !errors.Is(err, ErrDynamicProfileWouldEmpty) {
		t.Fatalf("err = %v, want ErrDynamicProfileWouldEmpty", err)
	}
	if !strings.Contains(err.Error(), "Solo") {
		t.Fatalf("err = %v, want the blocking dynamic profile name", err)
	}
	profile, err := repo.GetAgentProfile(context.Background(), fixture.candidateID)
	if err != nil || !profile.Enabled {
		t.Fatalf("target profile = %#v err = %v, want unchanged enabled", profile, err)
	}
	stored, routes, err := dynamicRepo.GetDynamicAgentProfile(context.Background(), fixture.dynamicID)
	if err != nil || stored.Version != 1 || len(routes) != 1 {
		t.Fatalf("dynamic profile changed: version=%d routes=%#v err=%v", stored.Version, routes, err)
	}
}

// TestResolveDynamicReferencesStaleVersionDeleteSkipsAutomation pins the
// delete-path preflight ordering: the read-only resolution builder runs before
// disableReferencingAutomations, so a stale version makes the whole delete
// request fail with the conflict contract without ever disabling a referencing
// automation.
func TestResolveDynamicReferencesStaleVersionDeleteSkipsAutomation(t *testing.T) {
	ctrl, repo, fixture := newResolutionFixture(t)
	dynamicRepo := repo.(store.DynamicProfileRepository)
	automations := &fakeAutomationDependencyChecker{refs: []AutomationReference{{ID: "auto-1", Name: "Weekly"}}}
	ctrl.automationDeps = automations

	_, err := ctrl.ResolveDynamicReferences(context.Background(), DynamicReferenceResolutionRequest{
		ProfileID: fixture.candidateID,
		Operation: "delete",
		Action:    "remove_candidate",
		Versions:  map[string]int64{fixture.dynamicID: 99},
	})
	if !errors.Is(err, ErrDynamicProfileVersionConflict) {
		t.Fatalf("err = %v, want ErrDynamicProfileVersionConflict", err)
	}
	if automations.disableCalls != 0 {
		t.Fatalf("automation disable calls = %d, want 0 (preflight must precede the mutation)", automations.disableCalls)
	}
	profile, err := repo.GetAgentProfile(context.Background(), fixture.candidateID)
	if err != nil || !profile.Enabled {
		t.Fatalf("target profile = %#v err = %v, want unchanged enabled", profile, err)
	}
	stored, routes, err := dynamicRepo.GetDynamicAgentProfile(context.Background(), fixture.dynamicID)
	if err != nil || stored.Version != 1 || len(routes) != 2 {
		t.Fatalf("dynamic profile changed: version=%d routes=%#v err=%v", stored.Version, routes, err)
	}
}

func TestResolveDynamicReferencesReferenceSetChangeRejects(t *testing.T) {
	ctrl, _, fixture := newResolutionFixture(t)
	_, err := ctrl.ResolveDynamicReferences(context.Background(), DynamicReferenceResolutionRequest{
		ProfileID: fixture.candidateID,
		Operation: "disable",
		Action:    "disable_candidate",
		Versions:  map[string]int64{fixture.dynamicID: 1, "ghost": 1},
	})
	if !errors.Is(err, ErrDynamicProfileVersionConflict) {
		t.Fatalf("err = %v, want ErrDynamicProfileVersionConflict", err)
	}
}

// resolutionFixture is one concrete candidate that is the tier head of one
// dynamic profile, plus a joined second candidate.
type resolutionFixture struct {
	candidateID string
	otherID     string
	dynamicID   string
}

// newResolutionHarness builds the controller, store, and two concrete candidate
// profiles shared by the resolution fixtures.
func newResolutionHarness(t *testing.T) (*Controller, store.Repository, *models.AgentProfile, *models.AgentProfile) {
	t.Helper()
	ctrl, repo := newSQLiteBackedController(t)
	if err := ctrl.agentRegistry.Register(agents.NewDynamicAgent()); err != nil {
		t.Fatalf("register dynamic agent: %v", err)
	}
	if err := ctrl.agentRegistry.Register(agents.NewClaudeACP()); err != nil {
		t.Fatalf("register concrete agent: %v", err)
	}
	ctrl.SetDynamicAgentRoutingEnabled(true)
	ctx := context.Background()
	if err := repo.CreateAgent(ctx, &models.Agent{ID: agents.DynamicAgentID, Name: agents.DynamicAgentID}); err != nil {
		t.Fatalf("create dynamic family: %v", err)
	}
	if err := repo.CreateAgent(ctx, &models.Agent{ID: "claude-family", Name: "claude-acp"}); err != nil {
		t.Fatalf("create concrete family: %v", err)
	}
	candidate := &models.AgentProfile{AgentID: "claude-family", Name: "Candidate", AgentDisplayName: "Claude"}
	if err := repo.CreateAgentProfile(ctx, candidate); err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	other := &models.AgentProfile{AgentID: "claude-family", Name: "Other", AgentDisplayName: "Claude"}
	if err := repo.CreateAgentProfile(ctx, other); err != nil {
		t.Fatalf("create other: %v", err)
	}
	return ctrl, repo, candidate, other
}

// newResolutionFixture builds the smallest dynamic document that exercises
// tier-head inheritance: candidate (head, pace) joined by other.
func newResolutionFixture(t *testing.T) (*Controller, store.Repository, resolutionFixture) {
	t.Helper()
	ctrl, repo, candidate, other := newResolutionHarness(t)
	pace := &dto.DynamicAgentTierPolicyDTO{Mode: "pace", OnFailure: "next_tier"}
	created, err := ctrl.CreateProfile(context.Background(), CreateProfileRequest{
		AgentID: agents.DynamicAgentID,
		Name:    "Balanced",
		Dynamic: &dto.DynamicAgentProfileDTO{Candidates: []dto.DynamicAgentCandidateDTO{
			{Position: 0, ExecutionProfileID: candidate.ID, Enabled: true, Policies: resolutionPolicy(false, pace)},
			{Position: 1, ExecutionProfileID: other.ID, Enabled: true, Policies: resolutionPolicy(true, nil)},
		}},
	})
	if err != nil {
		t.Fatalf("create dynamic profile: %v", err)
	}
	return ctrl, repo, resolutionFixture{
		candidateID: candidate.ID,
		otherID:     other.ID,
		dynamicID:   created.ID,
	}
}

// newSoleCandidateResolutionFixture builds a dynamic profile whose only
// candidate is the target, so a remove_candidate would empty it.
func newSoleCandidateResolutionFixture(t *testing.T) (*Controller, store.Repository, resolutionFixture) {
	t.Helper()
	ctrl, repo, candidate, other := newResolutionHarness(t)
	pace := &dto.DynamicAgentTierPolicyDTO{Mode: "pace", OnFailure: "next_tier"}
	created, err := ctrl.CreateProfile(context.Background(), CreateProfileRequest{
		AgentID: agents.DynamicAgentID,
		Name:    "Solo",
		Dynamic: &dto.DynamicAgentProfileDTO{Candidates: []dto.DynamicAgentCandidateDTO{
			{Position: 0, ExecutionProfileID: candidate.ID, Enabled: true, Policies: resolutionPolicy(false, pace)},
		}},
	})
	if err != nil {
		t.Fatalf("create dynamic profile: %v", err)
	}
	return ctrl, repo, resolutionFixture{
		candidateID: candidate.ID,
		otherID:     other.ID,
		dynamicID:   created.ID,
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
