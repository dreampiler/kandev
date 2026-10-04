package dashboard_test

import (
	"context"
	"errors"
	"testing"
	"time"

	taskmodels "github.com/kandev/kandev/internal/task/models"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"

	"github.com/kandev/kandev/internal/office/dashboard"
)

// stubQuestionLister returns a fixed answerable-question list.
type stubQuestionLister struct {
	bundles []taskmodels.ClarificationBundleSummary
}

func (s *stubQuestionLister) ListAnswerableClarificationsForSessions(
	context.Context, []string,
) ([]taskmodels.ClarificationBundleSummary, error) {
	return s.bundles, nil
}

// stubCircuitLister returns a fixed circuit list, or an error.
type stubCircuitLister struct {
	rows []tasksqlite.DynamicCircuitRow
	err  error
}

func (s stubCircuitLister) ListOpenDynamicCircuits(context.Context) ([]tasksqlite.DynamicCircuitRow, error) {
	return s.rows, s.err
}

func TestOverviewWorkspaceRouteNarrowsToOneWorkspace(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)
	deps.svc.SetOverviewScopeSource(stubScopeSource{scope: dashboard.OverviewScopeReachable})

	resp, err := deps.svc.GetWorkspaceOverview(context.Background(), "ws-kanban")
	if err != nil {
		t.Fatalf("workspace overview: %v", err)
	}
	if len(resp.Workspaces) != 1 || resp.Workspaces[0].WorkspaceID != "ws-kanban" {
		t.Fatalf("workspaces = %+v; want only ws-kanban", resp.Workspaces)
	}
	for _, event := range resp.Last24h {
		if event.WorkspaceID != "" && event.WorkspaceID != "ws-kanban" {
			t.Fatalf("event from another workspace leaked: %+v", event)
		}
	}
}

func TestOverviewWorkspaceRouteRefusesForeignWorkspace(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	// ws-hidden is outside the caller's workspace list, so a per-workspace read
	// must be indistinguishable from a workspace that does not exist.
	for _, id := range []string{"ws-hidden", "ws-missing", ""} {
		if _, err := deps.svc.GetWorkspaceOverview(context.Background(), id); !errors.Is(err, dashboard.ErrOverviewWorkspaceNotFound) {
			t.Fatalf("workspace %q error = %v; want ErrOverviewWorkspaceNotFound", id, err)
		}
	}
}

func TestOverviewBlockedCircuitsReportUnknownUntilWired(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if resp.System.BlockedAccountsTotal != nil {
		t.Fatalf("blocked_accounts_total = %v with no circuit source; want absent (unknown)", *resp.System.BlockedAccountsTotal)
	}
	if len(resp.BlockedCircuits) != 0 {
		t.Fatalf("blocked_circuits = %+v with no circuit source; want none", resp.BlockedCircuits)
	}
}

func TestOverviewBlockedCircuitsCountWithProviderHealth(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	now := time.Now().UTC()
	mustExec(t, deps, `INSERT INTO office_provider_health
		(workspace_id, provider_id, scope, scope_value, state, error_code, retry_at, backoff_step, updated_at)
		VALUES ('ws-office', 'anthropic', 'model', 'opus', 'degraded', 'quota', ?, 1, ?)`,
		now.Add(90*time.Minute), now)
	circuitUntil := now.Add(30 * time.Minute)
	deps.svc.SetDynamicCircuitLister(stubCircuitLister{rows: []tasksqlite.DynamicCircuitRow{
		{Key: "model:credential|claude-opus", State: "open", Code: "usage_limit", Until: circuitUntil, Strikes: 2},
	}})

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(resp.BlockedCircuits) != 1 || resp.BlockedCircuits[0].Scope != "model" ||
		resp.BlockedCircuits[0].ScopeValue != "credential|claude-opus" {
		t.Fatalf("blocked_circuits = %+v; want one model-scoped circuit", resp.BlockedCircuits)
	}
	if resp.System.BlockedAccountsTotal == nil {
		t.Fatal("blocked_accounts_total absent with a wired circuit source")
	}
	// One provider-health block plus one circuit.
	if *resp.System.BlockedAccountsTotal != 2 {
		t.Fatalf("blocked_accounts_total = %d; want 2", *resp.System.BlockedAccountsTotal)
	}
	if resp.System.EarliestUnblockAt == nil || !resp.System.EarliestUnblockAt.Equal(circuitUntil) {
		t.Fatalf("earliest_unblock_at = %v; want the circuit's %v", resp.System.EarliestUnblockAt, circuitUntil)
	}
}

func TestOverviewDynamicCircuitSourceFailureIsNotZero(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)
	deps.svc.SetDynamicCircuitLister(stubCircuitLister{err: dashboard.ErrDynamicCircuitsUnavailable})

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if resp.System.BlockedAccountsTotal != nil {
		t.Fatalf("blocked_accounts_total = %d with an unavailable source; want absent (unknown)",
			*resp.System.BlockedAccountsTotal)
	}
}

// collapseHumanItems is package-private; its behaviour is pinned through the
// overview response so the wire shape is what the test observes.
func TestOverviewNeedsHumanCollapsesSameQuestion(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	stub := &stubQuestionLister{bundles: []taskmodels.ClarificationBundleSummary{
		{PendingID: "p-old", SessionID: "s-run", TaskID: "t-run", QuestionID: "q1",
			CreatedAt: time.Now().UTC().Add(-time.Hour)},
		{PendingID: "p-new", SessionID: "s-run", TaskID: "t-run", QuestionID: "q1",
			CreatedAt: time.Now().UTC()},
		{PendingID: "p-other", SessionID: "s-run", TaskID: "t-run", QuestionID: "q2",
			CreatedAt: time.Now().UTC()},
	}}
	deps.svc.SetAnswerableQuestionLister(stub)

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(resp.NeedsHuman) != 2 {
		t.Fatalf("needs_human = %+v; want the repeated question merged and the other kept", resp.NeedsHuman)
	}
	var repeated *int
	for i, item := range resp.NeedsHuman {
		switch item.ID {
		case "p-new":
			if item.Count != 2 {
				t.Fatalf("merged item count = %d; want 2", item.Count)
			}
			repeated = &resp.NeedsHuman[i].Count
		case "p-other":
			if item.Count != 1 {
				t.Fatalf("distinct question count = %d; want 1", item.Count)
			}
		default:
			t.Fatalf("unexpected item %q", item.ID)
		}
	}
	if repeated == nil {
		t.Fatal("the newest occurrence of the repeated question must be the row to answer")
	}
}

func TestOverviewSystemReportsAppliedThresholds(t *testing.T) {
	deps := overviewFixture(t)
	deps.svc.SetWorkspaceLister(overviewLister())
	deps.svc.SetOverviewReader(deps.repo)

	resp, err := deps.svc.GetWorkspacesAggregate(context.Background())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	th := resp.System.ProblemThresholds
	if th == nil {
		t.Fatal("problem_thresholds absent; the client cannot explain the classification")
	}
	if th.NoOutputMinutes != 15 || th.WindowHours != 24 {
		t.Fatalf("problem_thresholds = %+v; want the limits overview_status.go applied", th)
	}
}
