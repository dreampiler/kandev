package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
	taskmodels "github.com/kandev/kandev/internal/task/models"
)

func TestOverviewTaskClassification(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	session := func(id, state string, started time.Time, errMsg string) *sqlite.OverviewSessionRow {
		return &sqlite.OverviewSessionRow{
			ID: id, TaskID: "task", State: state, StartedAt: started, UpdatedAt: started, ErrorMessage: errMsg,
		}
	}
	cases := []struct {
		name       string
		state      string
		blockers   int
		entered    time.Duration
		sessions   []*sqlite.OverviewSessionRow
		output     map[string]time.Time
		queued     int
		oldest     time.Duration
		wantStatus string
		wantReason string
	}{
		{
			name: "latest session failed", state: stateInProgress, entered: 30 * time.Minute,
			sessions:   []*sqlite.OverviewSessionRow{session("s1", sessionStateFailed, ago(5*time.Minute), "AI_APICallError: Rate limit exceeded\nstack")},
			wantStatus: OverviewStatusError, wantReason: reasonSessionFailed,
		},
		{
			name: "error message with no later output", state: stateInProgress, entered: 30 * time.Minute,
			sessions:   []*sqlite.OverviewSessionRow{session("s1", sessionStateWaitingForInput, ago(5*time.Minute), "boom")},
			wantStatus: OverviewStatusError, wantReason: reasonSessionError,
		},
		{
			name: "running without output", state: stateInProgress, entered: time.Hour,
			sessions:   []*sqlite.OverviewSessionRow{session("s1", sessionStateRunning, ago(40*time.Minute), "")},
			output:     map[string]time.Time{"s1": ago(20 * time.Minute)},
			wantStatus: OverviewStatusStalled, wantReason: reasonNoOutput,
		},
		{
			name: "running with recent output", state: stateInProgress, entered: time.Hour,
			sessions:   []*sqlite.OverviewSessionRow{session("s1", sessionStateRunning, ago(40*time.Minute), "")},
			output:     map[string]time.Time{"s1": ago(2 * time.Minute)},
			wantStatus: OverviewStatusRunning, wantReason: reasonWorking,
		},
		{
			name: "starting too long", state: stateInProgress, entered: time.Hour,
			sessions:   []*sqlite.OverviewSessionRow{session("s1", sessionStateStarting, ago(20*time.Minute), "")},
			wantStatus: OverviewStatusDelayed, wantReason: reasonStartingTooLong,
		},
		{
			name: "queued behind idle session", state: stateInProgress, entered: time.Hour,
			sessions: []*sqlite.OverviewSessionRow{session("s1", sessionStateWaitingForInput, ago(time.Hour), "")},
			queued:   2, oldest: 11 * time.Minute,
			wantStatus: OverviewStatusDelayed, wantReason: reasonQueueNotDelivered,
		},
		{
			name: "review step dwell", state: stateInReview, entered: 61 * time.Minute,
			wantStatus: OverviewStatusDelayed, wantReason: reasonStepDwell,
		},
		{
			name: "blocked within a day", state: stateBlocked, entered: time.Hour,
			wantStatus: OverviewStatusBlocked, wantReason: reasonOnHold,
		},
		{
			name: "waiting on prerequisite", state: stateTODO, blockers: 1, entered: time.Hour,
			wantStatus: OverviewStatusWaiting, wantReason: reasonWaitingPrereq,
		},
		{
			name: "never started", state: stateTODO, entered: time.Hour,
			wantStatus: OverviewStatusWaiting, wantReason: reasonNotStarted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &overviewTask{
				row: &sqlite.OverviewTaskRow{
					ID: "task", State: tc.state, OpenBlockers: tc.blockers, StepName: "Build",
					StepEnteredAt: now.Add(-tc.entered), UpdatedAt: now.Add(-tc.entered),
				},
				sessions: tc.sessions,
				queued:   tc.queued,
			}
			if tc.queued > 0 {
				task.oldestQueue = now.Add(-tc.oldest)
			}
			output := tc.output
			if output == nil {
				output = map[string]time.Time{}
			}
			task.classify(now, defaultOverviewThresholds, output)
			if task.status != tc.wantStatus || task.reason == nil || task.reason.Code != tc.wantReason {
				t.Fatalf("status = %q reason = %+v; want %q / %q", task.status, task.reason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

func TestOverviewErrorDetailIsFirstLineVerbatim(t *testing.T) {
	now := time.Now().UTC()
	task := &overviewTask{
		row: &sqlite.OverviewTaskRow{ID: "t", State: stateInProgress, StepEnteredAt: now},
		sessions: []*sqlite.OverviewSessionRow{{
			ID: "s", State: sessionStateFailed, StartedAt: now, UpdatedAt: now,
			ErrorMessage: "\n  AI_APICallError: Rate limit exceeded  \n at call (x.js:1)",
		}},
	}
	task.classify(now, defaultOverviewThresholds, map[string]time.Time{})
	if task.reason.Detail != "AI_APICallError: Rate limit exceeded" {
		t.Fatalf("detail = %q", task.reason.Detail)
	}
}

func TestQueueFirstLineStripsSystemBlocks(t *testing.T) {
	got := queueFirstLine("<kandev-system>\nhidden\n</kandev-system>\n\n  Please rerun the build  \nmore")
	if got != "Please rerun the build" {
		t.Fatalf("first line = %q", got)
	}
	if queueFirstLine("<kandev-system>unterminated") != "" {
		t.Fatal("an unterminated system block must not leak its content")
	}
}

func TestClassifyQueue(t *testing.T) {
	now := time.Now().UTC()
	row := func(state string, waited time.Duration) *sqlite.OverviewQueueRow {
		return &sqlite.OverviewQueueRow{SessionState: state, Oldest: now.Add(-waited), Count: 1}
	}
	cases := []struct {
		row  *sqlite.OverviewQueueRow
		want string
	}{
		{row(sessionStateCancelled, time.Minute), OverviewQueueUndeliverable},
		{row("", time.Minute), OverviewQueueUndeliverable},
		{row(sessionStateWaitingForInput, 11*time.Minute), OverviewQueueDelayed},
		{row(sessionStateRunning, 11*time.Minute), OverviewQueueWaiting},
		{row(sessionStateRunning, 16*time.Minute), OverviewQueueDelayed},
	}
	for _, tc := range cases {
		if got, _ := classifyQueue(tc.row, now, defaultOverviewThresholds); got != tc.want {
			t.Fatalf("state %q waited %v: got %q want %q", tc.row.SessionState, now.Sub(tc.row.Oldest), got, tc.want)
		}
	}
}

func TestAggregateWorkspacesScope(t *testing.T) {
	ws := workspacesForScopeTest()
	_, officeIDs := aggregateWorkspaces(ws, OverviewScopeOffice)
	if len(officeIDs) != 1 || officeIDs[0] != "office" {
		t.Fatalf("office scope ids = %v", officeIDs)
	}
	_, reachableIDs := aggregateWorkspaces(ws, OverviewScopeReachable)
	if len(reachableIDs) != 2 || reachableIDs[0] != "kanban" {
		t.Fatalf("reachable scope ids = %v, want both sorted by name", reachableIDs)
	}
}

func workspacesForScopeTest() []*taskmodels.Workspace {
	return []*taskmodels.Workspace{
		{ID: "office", Name: "Beta", OfficeWorkflowID: "wf"},
		{ID: "kanban", Name: "Alpha"},
		nil,
	}
}

// The failure breakdown shows the agent's own first line verbatim, so a line
// longer than the model card's 80-rune display cap must survive whole.
func TestSummarizeFailureBucketsKeepsFirstLineVerbatim(t *testing.T) {
	long := strings.Repeat("x", 200)
	rows := []*sqlite.OverviewFailureBucketRow{{
		WorkspaceID:  "ws",
		Bucket:       "no_response",
		ErrorMessage: "\n  " + long + "  \n stack detail below",
		Count:        2,
	}}
	_, samples := summarizeFailureBuckets(rows)
	got := samples["ws"]
	if len(got) != 1 || got[0].Kind != long || got[0].Count != 2 {
		t.Fatalf("samples = %+v, want one whole-first-line kind (len %d)", got, len(long))
	}
}

func TestFailureSampleKindSkipsBlankLines(t *testing.T) {
	if got := failureSampleKind("   \n\n  "); got != "" {
		t.Fatalf("blank message kind = %q, want empty", got)
	}
	if got := failureSampleKind("  first line  \nsecond"); got != "first line" {
		t.Fatalf("kind = %q, want the trimmed first line", got)
	}
}
