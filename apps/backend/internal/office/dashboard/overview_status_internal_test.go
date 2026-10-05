package dashboard

import (
	"context"
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
	// primary marks the session the task itself runs on, as opposed to an
	// auxiliary session it ran alongside.
	primary := func(s *sqlite.OverviewSessionRow) *sqlite.OverviewSessionRow {
		s.IsPrimary = true
		return s
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
		owner      bool
		wantStatus string
		wantReason string
	}{
		{
			name: "latest session failed", state: stateInProgress, entered: 30 * time.Minute,
			sessions:   []*sqlite.OverviewSessionRow{primary(session("s1", sessionStateFailed, ago(5*time.Minute), "AI_APICallError: Rate limit exceeded\nstack"))},
			wantStatus: OverviewStatusError, wantReason: reasonSessionFailed,
		},
		{
			name: "error message with no later output", state: stateInProgress, entered: 30 * time.Minute,
			sessions:   []*sqlite.OverviewSessionRow{primary(session("s1", sessionStateWaitingForInput, ago(5*time.Minute), "boom"))},
			wantStatus: OverviewStatusError, wantReason: reasonSessionError,
		},
		{
			name: "primary running while a newer auxiliary session failed", state: stateInProgress, entered: 10 * time.Minute,
			sessions: []*sqlite.OverviewSessionRow{
				primary(session("s1", sessionStateRunning, ago(40*time.Minute), "")),
				session("s2", sessionStateFailed, ago(5*time.Minute), "agent produced no output since start"),
			},
			output:     map[string]time.Time{"s1": ago(2 * time.Minute)},
			wantStatus: OverviewStatusRunning, wantReason: reasonWorking,
		},
		{
			name: "stopped on purpose while a prerequisite is open", state: stateInProgress, blockers: 1, entered: 30 * time.Minute,
			sessions:   []*sqlite.OverviewSessionRow{primary(session("s1", sessionStateCancelled, ago(3*time.Hour), "stopped by parent task via MCP"))},
			wantStatus: OverviewStatusWaiting, wantReason: reasonWaitingPrereq,
		},
		{
			name: "stopped on purpose without a prerequisite", state: stateInProgress, entered: 30 * time.Minute,
			sessions:   []*sqlite.OverviewSessionRow{primary(session("s1", sessionStateCancelled, ago(3*time.Hour), "stopped via API"))},
			wantStatus: OverviewStatusWaiting, wantReason: reasonSessionEnded,
		},
		{
			name: "failure from before the current step", state: stateInProgress, entered: 30 * time.Minute,
			sessions:   []*sqlite.OverviewSessionRow{primary(session("s1", sessionStateFailed, ago(2*time.Hour), "AI_APICallError: Rate limit exceeded"))},
			wantStatus: OverviewStatusWaiting, wantReason: reasonSessionEnded,
		},
		{
			name: "blocked past a day", state: stateBlocked, entered: 25 * time.Hour,
			wantStatus: OverviewStatusBlocked, wantReason: reasonOnHold,
		},
		{
			name: "waiting on a prerequisite past a day", state: stateInProgress, blockers: 1, entered: 30 * time.Hour,
			wantStatus: OverviewStatusWaiting, wantReason: reasonWaitingPrereq,
		},
		{
			name: "working step past the dwell limit", state: stateInProgress, entered: 3 * time.Hour,
			wantStatus: OverviewStatusDelayed, wantReason: reasonStepDwell,
		},
		{
			name: "scheduling past the not-advancing limit", state: taskStateScheduling, entered: 21 * time.Hour,
			wantStatus: OverviewStatusDelayed, wantReason: reasonNotAdvancing,
		},
		{
			name: "unanswered owner question past the dwell limit", state: stateInProgress, entered: 3 * time.Hour,
			sessions:   []*sqlite.OverviewSessionRow{primary(session("s1", sessionStateIdle, ago(3*time.Hour), ""))},
			owner:      true,
			wantStatus: OverviewStatusWaiting, wantReason: reasonIdle,
		},
		{
			name: "a deliberate stop does not hide a genuine recent failure", state: stateInProgress, entered: 3 * time.Hour,
			sessions: []*sqlite.OverviewSessionRow{
				primary(session("s1", sessionStateCancelled, ago(3*time.Hour), "stopped via API")),
				session("s2", sessionStateFailed, ago(2*time.Hour), "AI_APICallError: Rate limit exceeded"),
			},
			wantStatus: OverviewStatusError, wantReason: reasonRecentFailures,
		},
		{
			name: "an unanswered owner question keeps a recent failure off the error list", state: stateInProgress, entered: 3 * time.Hour,
			sessions: []*sqlite.OverviewSessionRow{
				primary(session("s1", sessionStateCancelled, ago(2*time.Hour), "stopped via API")),
				session("s2", sessionStateFailed, ago(time.Hour), "AI_APICallError: Rate limit exceeded"),
			},
			owner:      true,
			wantStatus: OverviewStatusWaiting, wantReason: reasonSessionEnded,
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
				sessions:      tc.sessions,
				queued:        tc.queued,
				awaitingOwner: tc.owner,
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

// fakeQuestionLister answers with fixed bundles and records the sessions it was
// asked about, so a test can prove the overview asks once and classifies from
// that answer.
type fakeQuestionLister struct {
	bundles []taskmodels.ClarificationBundleSummary
	asked   [][]string
}

func (f *fakeQuestionLister) ListAnswerableClarificationsForSessions(
	_ context.Context, sessionIDs []string,
) ([]taskmodels.ClarificationBundleSummary, error) {
	f.asked = append(f.asked, sessionIDs)
	return f.bundles, nil
}

// TestOverviewBaselineIsTheTaskOwnSession pins the mechanism behind the verdict
// rather than only its outcome: the session the verdict is read from is the
// task's own, so a failed auxiliary session started after it cannot become the
// task's condition.
func TestOverviewBaselineIsTheTaskOwnSession(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	task := &overviewTask{
		row: &sqlite.OverviewTaskRow{
			ID: "task", State: stateInProgress, StepName: "Reconcile",
			StepEnteredAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-10 * time.Minute),
		},
		sessions: []*sqlite.OverviewSessionRow{
			{ID: "primary", TaskID: "task", State: sessionStateRunning, IsPrimary: true, StartedAt: now.Add(-40 * time.Minute), UpdatedAt: now.Add(-40 * time.Minute)},
			{ID: "aux", TaskID: "task", State: sessionStateFailed, StartedAt: now.Add(-5 * time.Minute), UpdatedAt: now.Add(-5 * time.Minute), ErrorMessage: "agent produced no output since start"},
		},
	}
	task.classify(now, defaultOverviewThresholds, map[string]time.Time{"primary": now.Add(-2 * time.Minute)})
	if task.baseline == nil || task.baseline.ID != "primary" {
		t.Fatalf("baseline = %+v; want the task's own session", task.baseline)
	}
	if task.status != OverviewStatusRunning {
		t.Fatalf("status = %q reason = %+v; want running", task.status, task.reason)
	}
}

// approvalsOnlyReader answers the pending-approval read and nothing else, so a
// test can render the needs-human list without standing up the whole overview.
type approvalsOnlyReader struct {
	OverviewReader
}

func (approvalsOnlyReader) ListOverviewPendingApprovals(
	_ context.Context, _ []string, _ int,
) ([]*sqlite.OverviewApprovalRow, error) {
	return nil, nil
}

// TestOverviewAnswerableQuestionsFeedClassification pins that one read of the
// answerable questions both exempts the task from a delay verdict and fills the
// needs-human list, so a task waiting on the owner stops counting as a problem
// without losing its place in the list of things to answer.
func TestOverviewAnswerableQuestionsFeedClassification(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	lister := &fakeQuestionLister{bundles: []taskmodels.ClarificationBundleSummary{{
		PendingID: "p1", QuestionID: "q1", TaskID: "task", SessionID: "s1", CreatedAt: now.Add(-time.Hour),
	}}}
	svc := &DashboardService{}
	svc.SetOverviewReader(approvalsOnlyReader{})
	svc.SetAnswerableQuestionLister(lister)
	snap := &overviewSnapshot{
		resp: &WorkspaceAggregateResponse{},
		now:  now,
		tasks: []*overviewTask{{
			row: &sqlite.OverviewTaskRow{
				ID: "task", WorkspaceID: "ws", Title: "Build", State: stateInProgress, StepName: "Build",
				StepEnteredAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour),
			},
			sessions: []*sqlite.OverviewSessionRow{{
				ID: "s1", TaskID: "task", State: sessionStateIdle, IsPrimary: true,
				StartedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour),
			}},
		}},
	}
	if err := svc.loadAnswerableQuestions(context.Background(), snap); err != nil {
		t.Fatalf("loadAnswerableQuestions: %v", err)
	}
	task := snap.tasks[0]
	task.classify(now, defaultOverviewThresholds, map[string]time.Time{})
	if task.status == OverviewStatusDelayed {
		t.Fatalf("a task waiting on the owner must not read as delayed, got %q", task.status)
	}
	if !task.awaitingOwner {
		t.Fatal("the task owning the question must be recorded as awaiting its owner")
	}
	if err := svc.assembleNeedsHuman(context.Background(), snap, []string{"ws"}); err != nil {
		t.Fatalf("assembleNeedsHuman: %v", err)
	}
	if len(snap.resp.NeedsHuman) != 1 || snap.resp.NeedsHuman[0].TaskID != "task" {
		t.Fatalf("needs human = %+v; want the owner's question", snap.resp.NeedsHuman)
	}
	if len(lister.asked) != 1 {
		t.Fatalf("the questions were read %d times; the overview reads them once per pass", len(lister.asked))
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
