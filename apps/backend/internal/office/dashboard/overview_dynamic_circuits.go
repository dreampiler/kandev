package dashboard

import (
	"context"
	"errors"
	"net/http"
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
	for _, circuit := range circuits {
		scope, value := splitCircuitKey(circuit.Key)
		snap.resp.BlockedCircuits = append(snap.resp.BlockedCircuits, OverviewBlockedCircuit{
			ResourceKey: circuit.Key, Scope: scope, ScopeValue: value,
			State: circuit.State, Code: circuit.Code, Strikes: circuit.Strikes,
			Until: timePtr(circuit.Until),
		})
	}
	return nil
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
