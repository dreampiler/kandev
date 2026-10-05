package dashboard

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// ErrDynamicCircuitsUnavailable is returned when the overview cannot read the
// dynamic-routing circuits. It is reported as its own state so the client
// shows "unknown" instead of reading an absent source as no blocks.
var ErrDynamicCircuitsUnavailable = errors.New("dynamic routing circuits are not configured")

// DynamicCircuitLister lists the open dynamic-routing resource circuits.
// Implemented by the task repository, which owns dynamic_resource_circuits.
type DynamicCircuitLister interface {
	ListOpenDynamicCircuits(ctx context.Context) ([]tasksqlite.DynamicCircuitRow, error)
}

// SetDynamicCircuitLister wires the dynamic-circuit source. Without it the
// overview reports the blocked-circuits state as unavailable rather than empty.
func (s *DashboardService) SetDynamicCircuitLister(l DynamicCircuitLister) { s.circuitLister = l }

// dynamicResourceKeySeparator splits a circuit key of the form "<scope>:<value>".
const dynamicResourceKeySeparator = ":"

// splitCircuitKey separates a circuit's scope from the fingerprint it holds.
// A key with no separator is reported whole as the scope value, so an
// unrecognized key is still shown rather than dropped.
func splitCircuitKey(key string) (scope, value string) {
	if before, after, found := strings.Cut(key, dynamicResourceKeySeparator); found {
		return before, after
	}
	return "", key
}

// assembleDynamicCircuits reads the open circuits onto the response. An
// unwired or failing source leaves the rows absent and the availability flag
// false, so the client reports the state as unknown rather than zero. The
// blocked totals are folded in later, with the provider-health blocks, so both
// sources contribute to one count and one clear-at instant.
func (s *DashboardService) assembleDynamicCircuits(ctx context.Context, snap *overviewSnapshot) error {
	if s.circuitLister == nil {
		return nil
	}
	circuits, err := s.circuitLister.ListOpenDynamicCircuits(ctx)
	if err != nil {
		if errors.Is(err, ErrDynamicCircuitsUnavailable) {
			return nil
		}
		return err
	}
	snap.circuitsAvailable = true
	identities := make([]circuitIdentity, 0, len(circuits))
	for _, circuit := range circuits {
		scope, value := splitCircuitKey(circuit.Key)
		identity := circuitIdentityOf(circuit.Key)
		identities = append(identities, identity)
		snap.resp.BlockedCircuits = append(snap.resp.BlockedCircuits, OverviewBlockedCircuit{
			ResourceKey: circuit.Key, Scope: scope, ScopeValue: value,
			State: circuit.State, Code: circuit.Code, Strikes: circuit.Strikes,
			Until: timePtr(circuit.Until), Blocking: circuitBlocking(circuit.State, circuit.Until, snap.now),
			ProfileID: identity.profileID, ModelName: identity.modelName,
		})
	}
	s.nameCircuits(ctx, snap, identities)
	return nil
}

// nameCircuits attaches the agent profile name of every circuit that names a
// profile. The profile ids come from the circuit keys themselves, so this reads
// no routing state and no new table: it is the same profile read the model cards
// use. A read that fails, or a profile that no longer resolves, leaves that
// circuit unnamed, which the screen reports as unidentifiable.
func (s *DashboardService) nameCircuits(ctx context.Context, snap *overviewSnapshot, identities []circuitIdentity) {
	missing := unresolvedCircuitProfileIDs(snap, identities)
	if len(missing) > 0 && s.overviewReader != nil {
		sort.Strings(missing)
		if profiles, err := s.overviewReader.ListOverviewProfiles(ctx, missing); err == nil {
			if snap.profileNames == nil {
				snap.profileNames = map[string]string{}
			}
			for _, profile := range profiles {
				snap.profileNames[profile.ID] = profileDisplayName(profile)
			}
		}
	}
	for i := range snap.resp.BlockedCircuits {
		circuit := &snap.resp.BlockedCircuits[i]
		circuit.ProfileName = snap.profileNames[circuit.ProfileID]
	}
}

// unresolvedCircuitProfileIDs collects the profile ids the snapshot cannot name
// yet, so the profile read covers exactly the circuits that still need it.
func unresolvedCircuitProfileIDs(snap *overviewSnapshot, identities []circuitIdentity) []string {
	missing := make([]string, 0, len(identities))
	seen := map[string]bool{}
	for _, identity := range identities {
		if identity.profileID == "" || snap.profileNames[identity.profileID] != "" || seen[identity.profileID] {
			continue
		}
		seen[identity.profileID] = true
		missing = append(missing, identity.profileID)
	}
	return missing
}

// GetWorkspaceOverview returns the overview narrowed to one workspace of the
// caller's scope. Each workspace is computed and cached on its own, so a slow
// workspace cannot hold back the ones that are ready.
func (s *DashboardService) GetWorkspaceOverview(
	ctx context.Context, workspaceID string,
) (*WorkspaceAggregateResponse, error) {
	snap, err := s.loadWorkspaceSnapshot(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return snap.resp, nil
}

// getWorkspaceOverview serves GET /workspaces/aggregate/workspace.
func (h *Handler) getWorkspaceOverview(c *gin.Context) {
	resp, err := h.svc.GetWorkspaceOverview(c.Request.Context(), c.Query("workspace_id"))
	switch {
	case err == nil:
		c.JSON(http.StatusOK, resp)
	case errors.Is(err, ErrOverviewWorkspaceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrWorkspaceAggregateUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
