package dashboard

import (
	"context"
	"encoding/json"
	"expvar"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
	taskmodels "github.com/kandev/kandev/internal/task/models"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// Office overview scopes (the user setting office_overview_scope).
const (
	OverviewScopeOffice    = "office"
	OverviewScopeReachable = "reachable"
)

// overviewPendingApprovalLimit bounds the approvals listed under "needs a
// person"; the count still covers every pending approval.
const overviewPendingApprovalLimit = 50

// overviewEventKindLimit bounds one kind's contribution to the last-24-hours
// list and overviewEventMergeLimit the merged list. Both sit at or above the 50
// rows the screen shows, because that screen filters by kind and only then caps
// what it renders: a kind cut short here could never be reached through its
// chip.
const (
	overviewEventKindLimit  = 50
	overviewEventMergeLimit = 250
	overviewParentLimit     = 5
)

// overviewEventMoveLimit bounds the step-move rows one pass groups into task
// runs. A run is folded from its task's rows, so this bounds how much movement a
// single pass reconstructs rather than how many runs reach the list; the run
// count stays far below it because rows are grouped, not listed.
const overviewEventMoveLimit = 2000

// OverviewReader is the read-only persistence surface behind the overview.
// Implemented by the Office sqlite repository.
type OverviewReader interface {
	ListOverviewOpenTasks(ctx context.Context, workspaceIDs []string) ([]*sqlite.OverviewTaskRow, error)
	CountOverviewChildren(ctx context.Context, parentIDs []string) (map[string]int, error)
	ListOverviewSessionsForTasks(ctx context.Context, taskIDs []string) ([]*sqlite.OverviewSessionRow, error)
	LastAgentOutputBySession(ctx context.Context, sessionIDs []string) (map[string]time.Time, error)
	ListOverviewQueues(ctx context.Context, workspaceIDs []string) ([]*sqlite.OverviewQueueRow, error)
	FirstQueuedMessageBySession(ctx context.Context, sessionIDs []string) (map[string]string, error)
	ListOverviewCompleted(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) (map[string]int, []*sqlite.OverviewCompletedRow, error)
	ListOverviewProfileSessions(
		ctx context.Context, workspaceIDs []string, since time.Time,
	) ([]*sqlite.OverviewProfileSessionRow, error)
	ListOverviewProfiles(ctx context.Context, profileIDs []string) ([]*sqlite.OverviewProfileRow, error)
	ListOverviewBlockedProviders(ctx context.Context, workspaceIDs []string) ([]*sqlite.OverviewBlockedProviderRow, error)
	ListOverviewPendingApprovals(ctx context.Context, workspaceIDs []string, limit int) ([]*sqlite.OverviewApprovalRow, error)
	ListOverviewAutomationTasks(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) ([]*sqlite.OverviewAutomationTaskRow, error)
	ListOverviewCreatedTasks(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) ([]*sqlite.OverviewCreatedTaskRow, error)

	// The last-24-hours sources no earlier query answered.
	ListOverviewModelBlocks(
		ctx context.Context, since, now time.Time, limit int,
	) ([]*sqlite.OverviewModelBlockRow, error)
	ListOverviewMergedPRs(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) ([]*sqlite.OverviewMergedPRRow, error)
	ListOverviewAutomationFailures(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) ([]*sqlite.OverviewAutomationFailureRow, error)
	ListOverviewDecisions(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) ([]*sqlite.OverviewDecisionRow, error)
	ListOverviewStepTransitions(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) ([]*sqlite.OverviewStepTransitionRow, error)
	ListOverviewFailureFollowups(
		ctx context.Context, workspaceIDs []string, since time.Time, limit int,
	) ([]*sqlite.OverviewFailureFollowupRow, error)
}

// AnswerableQuestionLister lists answerable clarification bundles of the
// given sessions. Implemented by the task repository so the overview shares
// the inbox's definition of "answerable".
type AnswerableQuestionLister interface {
	ListAnswerableClarificationsForSessions(
		ctx context.Context, sessionIDs []string,
	) ([]taskmodels.ClarificationBundleSummary, error)
}

// OverviewScopeSource reads the caller's overview scope setting.
type OverviewScopeSource interface {
	OfficeOverviewScope(ctx context.Context) (string, error)
}

// SessionCapacityReading is one bounded reading of the instance's session
// admission: how many sessions each lane holds and the limit that lane is
// admitted against.
type SessionCapacityReading struct {
	// GeneralUsed is the general lane's population and ControlUsed the control
	// lane's, so the two report the split rather than one total the reader has
	// to unpick. A population that could not be read reports Known=false while
	// still carrying the limits the controller enforces.
	GeneralUsed     int
	GeneralLimit    int
	ControlUsed     int
	ControlLimit    int
	ControlEnabled  bool
	PopulationKnown bool
}

// SessionCapacityReader reads the live session admission state. It is read on
// every overview pass rather than recorded at start, so a limit changed in
// Settings reaches the next refresh without a restart.
type SessionCapacityReader interface {
	CurrentSessionCapacity(ctx context.Context) (SessionCapacityReading, error)
}

// SetOverviewReader wires the overview read surface. Without it the aggregate
// keeps its original Office-only counts and the list routes respond 503.
func (s *DashboardService) SetOverviewReader(r OverviewReader) { s.overviewReader = r }

// SetAnswerableQuestionLister wires the answerable-question source.
func (s *DashboardService) SetAnswerableQuestionLister(l AnswerableQuestionLister) {
	s.questionLister = l
}

// SetOverviewScopeSource wires the per-user scope setting. Without it the
// overview uses the Office scope.
func (s *DashboardService) SetOverviewScopeSource(src OverviewScopeSource) { s.scopeSource = src }

// SetSessionCapacityReader wires the live session-admission reading. Without it
// the overview reports no capacity at all, so an unwired service never presents
// an absent reading as a measured zero.
func (s *DashboardService) SetSessionCapacityReader(r SessionCapacityReader) {
	s.capacityReader = r
}

// readSessionCapacity returns the current admission reading. An unwired or
// failing reader yields an unknown reading: the limits are left unreported
// rather than guessed, because a stale or invented limit is the defect this
// reading exists to remove.
func (s *DashboardService) readSessionCapacity(ctx context.Context) (SessionCapacityReading, bool) {
	if s.capacityReader == nil {
		return SessionCapacityReading{}, false
	}
	reading, err := s.capacityReader.CurrentSessionCapacity(ctx)
	if err != nil {
		return SessionCapacityReading{}, false
	}
	return reading, true
}

// SetBuildInfo records the running binary's version for the overview's
// server-start row. It is a setter rather than a constructor argument so the
// dashboard keeps one composition signature, and an unwired service reports no
// version rather than a placeholder one.
func (s *DashboardService) SetBuildInfo(version string) { s.buildVersion = version }

// overviewSnapshot is one computed overview, shared by the summary response
// and the list routes for the cache lifetime.
type overviewSnapshot struct {
	resp         *WorkspaceAggregateResponse
	now          time.Time
	names        map[string]string
	tasks        []*overviewTask
	sessions     []*sqlite.OverviewSessionRow // RUNNING or STARTING
	queues       []*sqlite.OverviewQueueRow
	completed    []*sqlite.OverviewCompletedRow
	lastOutput   map[string]time.Time
	profileNames map[string]string
	// accountIDs maps an agent profile to the provider account it authenticates
	// with, so the models section can group by account. Absent leaves every
	// model without an account rather than inventing one.
	accountIDs map[string]string
	taskTitles map[string]string

	firstLinesOnce sync.Once
	firstLines     map[string]string
	firstLinesErr  error

	// circuitsAvailable records whether the dynamic-circuit source answered.
	// Absent means unknown, never "no circuits".
	circuitsAvailable bool

	// followups maps a failed session id to what followed it, read on this same
	// pass so the reported recovery advances whenever the screen does.
	followups map[string]*sqlite.OverviewFailureFollowupRow
	// questions are the answerable bundles of this pass's live sessions, read
	// before classification so a task waiting on the owner is classified from
	// the same read the needs-human list renders.
	questions []taskmodels.ClarificationBundleSummary
	// buildVersion is the running binary's version for this pass, empty when
	// nothing wired one.
	buildVersion string
}

// overviewScope resolves the caller's scope, falling back to Office.
func (s *DashboardService) overviewScope(ctx context.Context) string {
	if s.scopeSource == nil {
		return OverviewScopeOffice
	}
	scope, err := s.scopeSource.OfficeOverviewScope(ctx)
	if err != nil || scope != OverviewScopeReachable {
		return OverviewScopeOffice
	}
	return scope
}

// loadOverviewSnapshot returns the caller's cached snapshot or builds it.
func (s *DashboardService) loadOverviewSnapshot(ctx context.Context) (*overviewSnapshot, error) {
	if s.workspaceLister == nil {
		return nil, ErrWorkspaceAggregateUnavailable
	}
	scope := s.overviewScope(ctx)
	key := overviewCallerKey(ctx) + "|" + scope
	return s.overviewCacheOrInit().get(ctx, key, func(ctx context.Context) (*overviewSnapshot, error) {
		return s.buildOverviewSnapshot(ctx, scope, "")
	})
}

// loadWorkspaceSnapshot returns the snapshot narrowed to one workspace of the
// caller's scope, cached under its own key so the per-workspace reads compute
// and expire independently of the whole-scope one.
func (s *DashboardService) loadWorkspaceSnapshot(
	ctx context.Context, workspaceID string,
) (*overviewSnapshot, error) {
	if s.workspaceLister == nil {
		return nil, ErrWorkspaceAggregateUnavailable
	}
	if workspaceID == "" {
		return nil, ErrOverviewWorkspaceNotFound
	}
	scope := s.overviewScope(ctx)
	key := overviewCallerKey(ctx) + "|" + scope + "|ws:" + workspaceID
	return s.overviewCacheOrInit().get(ctx, key, func(ctx context.Context) (*overviewSnapshot, error) {
		return s.buildOverviewSnapshot(ctx, scope, workspaceID)
	})
}

func (s *DashboardService) overviewCacheOrInit() *overviewCache {
	s.overviewOnce.Do(func() {
		if s.overview == nil {
			s.overview = newOverviewCache(overviewCacheTTL)
		}
	})
	return s.overview
}

// buildOverviewSnapshot computes the overview for the caller's scope, or for
// one workspace of it when only is set. A workspace outside the caller's scope
// answers ErrOverviewWorkspaceNotFound rather than an empty overview, so a
// foreign or nonexistent workspace is indistinguishable from a miss.
func (s *DashboardService) buildOverviewSnapshot(
	ctx context.Context, scope, only string,
) (*overviewSnapshot, error) {
	started := time.Now()
	workspaces, err := s.workspaceLister.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	ordered, ids := aggregateWorkspaces(workspaces, scope)
	if only != "" {
		ordered, ids = narrowToWorkspace(ordered, only)
		if len(ids) == 0 {
			return nil, ErrOverviewWorkspaceNotFound
		}
	}
	resp, err := s.buildAggregateBase(ctx, ordered, ids)
	if err != nil {
		return nil, err
	}
	snap := &overviewSnapshot{resp: resp, now: started.UTC(), names: map[string]string{}, buildVersion: s.buildVersion}
	for _, w := range ordered {
		snap.names[w.ID] = w.Name
	}
	resp.Scope = scope
	if s.overviewReader != nil {
		if err := s.fillOverview(ctx, snap, ids); err != nil {
			return nil, err
		}
	}
	resp.GeneratedAt = snap.now
	resp.ComputeMs = time.Since(started).Milliseconds()
	return snap, nil
}

// fillOverview runs the overview reads one after another on the read-only
// handle and assembles every section from them.
func (s *DashboardService) fillOverview(ctx context.Context, snap *overviewSnapshot, ids []string) error {
	th := defaultOverviewThresholds
	since := snap.now.Add(-th.Window)
	if err := s.loadOverviewTasks(ctx, snap, ids, th); err != nil {
		return err
	}
	counts, completed, err := s.overviewReader.ListOverviewCompleted(ctx, ids, since, 0)
	if err != nil {
		return err
	}
	snap.completed = completed
	childCounts, err := s.overviewReader.CountOverviewChildren(ctx, overviewParentIDs(snap.tasks))
	if err != nil {
		return err
	}
	assembleWorkspaceMetrics(snap, counts, childCounts)
	if err := s.assembleModels(ctx, snap, ids, since); err != nil {
		return err
	}
	if err := s.assembleNeedsHuman(ctx, snap, ids); err != nil {
		return err
	}
	if err := s.assembleDynamicCircuits(ctx, snap); err != nil {
		return err
	}
	automation, err := s.overviewReader.ListOverviewAutomationTasks(ctx, ids, since, overviewEventKindLimit)
	if err != nil {
		return err
	}
	created, err := s.overviewReader.ListOverviewCreatedTasks(ctx, ids, since, overviewEventKindLimit)
	if err != nil {
		return err
	}
	extra := s.readExtraEvents(ctx, snap, ids, since)
	capacity, capacityKnown := s.readSessionCapacity(ctx)
	assembleSystem(snap, capacity, capacityKnown, th)
	snap.resp.Last24h = assembleEvents(snap, automation, created, extra, since)
	return nil
}

// readExtraEvents reads the last-24-hours sources the original event merge does
// not cover, plus the failure follow-ups the failed rows carry. Model blocks are
// instance-wide rather than workspace-scoped, so they are read once per pass
// and every event they produce is install-wide by nature.
//
// Each source is read independently and a source that cannot answer contributes
// no events rather than failing the read, which is how the blocked-circuits
// source already behaves: these tables are owned by the task, automation, and
// code-host schemas rather than by this one, so an install whose schema predates
// one of them would otherwise lose the whole overview over a supplementary
// section. A missing source omits its kind; it never reports that kind as empty.
func (s *DashboardService) readExtraEvents(
	ctx context.Context, snap *overviewSnapshot, ids []string, since time.Time,
) *overviewExtraEvents {
	out := &overviewExtraEvents{}
	out.blocks = readOptionalEventSource(func() ([]*sqlite.OverviewModelBlockRow, error) {
		return s.overviewReader.ListOverviewModelBlocks(ctx, since, snap.now, overviewEventKindLimit)
	})
	s.nameBlockSubjects(ctx, snap, out.blocks)
	out.mergedPRs = readOptionalEventSource(func() ([]*sqlite.OverviewMergedPRRow, error) {
		return s.overviewReader.ListOverviewMergedPRs(ctx, ids, since, overviewEventKindLimit)
	})
	out.automationFailures = readOptionalEventSource(func() ([]*sqlite.OverviewAutomationFailureRow, error) {
		return s.overviewReader.ListOverviewAutomationFailures(ctx, ids, since, overviewEventKindLimit)
	})
	out.decisions = readOptionalEventSource(func() ([]*sqlite.OverviewDecisionRow, error) {
		return s.overviewReader.ListOverviewDecisions(ctx, ids, since, overviewEventKindLimit)
	})
	out.stepMoves = readOptionalEventSource(func() ([]*sqlite.OverviewStepTransitionRow, error) {
		return s.overviewReader.ListOverviewStepTransitions(ctx, ids, since, overviewEventMoveLimit)
	})
	s.indexFailureFollowups(ctx, snap, readOptionalEventSource(func() ([]*sqlite.OverviewFailureFollowupRow, error) {
		return s.overviewReader.ListOverviewFailureFollowups(ctx, ids, since, overviewEventKindLimit)
	}))
	return out
}

// readOptionalEventSource runs one supplementary read and yields nothing when it
// fails, so one unavailable source cannot take the overview down with it.
func readOptionalEventSource[T any](read func() ([]*T, error)) []*T {
	rows, err := read()
	if err != nil {
		return nil
	}
	return rows
}

// indexFailureFollowups records what followed each failed session and names the
// profiles the follow-up sessions ran on, so a failed row can report the model
// that took over without a second lookup per row. A profile that no longer
// resolves leaves its session unnamed rather than failing the read.
func (s *DashboardService) indexFailureFollowups(
	ctx context.Context, snap *overviewSnapshot, rows []*sqlite.OverviewFailureFollowupRow,
) {
	if len(rows) == 0 {
		return
	}
	snap.followups = make(map[string]*sqlite.OverviewFailureFollowupRow, len(rows))
	profileIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		snap.followups[row.SessionID] = row
		if row.NextProfileID != "" {
			profileIDs = append(profileIDs, row.NextProfileID)
		}
	}
	// A profile read that fails leaves the model unnamed, which the client
	// reports as unknown rather than dropping the follow-up it belongs to.
	_ = s.loadProfileNames(ctx, snap, profileIDs)
}

// loadOverviewTasks reads the open tasks, their sessions, last outputs and
// queues, then classifies every task.
func (s *DashboardService) loadOverviewTasks(
	ctx context.Context, snap *overviewSnapshot, ids []string, th overviewThresholds,
) error {
	rows, err := s.overviewReader.ListOverviewOpenTasks(ctx, ids)
	if err != nil {
		return err
	}
	byID := make(map[string]*overviewTask, len(rows))
	taskIDs := make([]string, 0, len(rows))
	snap.taskTitles = make(map[string]string, len(rows))
	for _, row := range rows {
		t := &overviewTask{row: row, automation: stepAutomation(row)}
		byID[row.ID] = t
		snap.tasks = append(snap.tasks, t)
		taskIDs = append(taskIDs, row.ID)
		snap.taskTitles[row.ID] = row.Title
	}
	sessions, err := s.overviewReader.ListOverviewSessionsForTasks(ctx, taskIDs)
	if err != nil {
		return err
	}
	outputIDs := groupOverviewSessions(snap, byID, sessions)
	if snap.lastOutput, err = s.overviewReader.LastAgentOutputBySession(ctx, outputIDs); err != nil {
		return err
	}
	if snap.queues, err = s.overviewReader.ListOverviewQueues(ctx, ids); err != nil {
		return err
	}
	for _, q := range snap.queues {
		if t := byID[q.TaskID]; t != nil {
			t.queued += q.Count
			if t.oldestQueue.IsZero() || q.Oldest.Before(t.oldestQueue) {
				t.oldestQueue = q.Oldest
			}
		}
		if _, ok := snap.taskTitles[q.TaskID]; !ok {
			snap.taskTitles[q.TaskID] = q.TaskTitle
		}
	}
	if err := s.loadAnswerableQuestions(ctx, snap); err != nil {
		return err
	}
	for _, t := range snap.tasks {
		t.classify(snap.now, th, snap.lastOutput)
	}
	applyChildStatus(snap.tasks)
	return nil
}

// stepAutomation reads the step's own configuration from the row the open-task
// query already joined. A step whose events cannot be read is treated as one
// that starts nothing by itself, which reports the task as waiting for a person
// rather than inventing progress.
func stepAutomation(row *sqlite.OverviewTaskRow) wfmodels.StepAutomation {
	var events wfmodels.StepEvents
	if row.StepEventsRaw != "" {
		if err := json.Unmarshal([]byte(row.StepEventsRaw), &events); err != nil {
			events = wfmodels.StepEvents{}
		}
	}
	return wfmodels.StepAutomation{OnEnter: events.OnEnter, PullFromStepID: row.StepPullFromStepID}
}

// groupOverviewSessions attaches sessions to their tasks, records the running
// ones, and returns the ids whose last output is worth looking up: every
// live session plus each task's newest session.
func groupOverviewSessions(
	snap *overviewSnapshot, byID map[string]*overviewTask, sessions []*sqlite.OverviewSessionRow,
) []string {
	newest := map[string]*sqlite.OverviewSessionRow{}
	var ids []string
	for _, sess := range sessions {
		t := byID[sess.TaskID]
		if t == nil {
			continue
		}
		t.sessions = append(t.sessions, sess)
		if cur := newest[sess.TaskID]; cur == nil || sess.StartedAt.After(cur.StartedAt) {
			newest[sess.TaskID] = sess
		}
		if isLiveSessionState(sess.State) {
			ids = append(ids, sess.ID)
		}
		if sess.State == sessionStateRunning || sess.State == sessionStateStarting {
			snap.sessions = append(snap.sessions, sess)
		}
	}
	for _, sess := range newest {
		if !isLiveSessionState(sess.State) {
			ids = append(ids, sess.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// narrowToWorkspace keeps only the named workspace of an already ordered,
// already scoped list.
func narrowToWorkspace(ordered []*taskmodels.Workspace, workspaceID string) ([]*taskmodels.Workspace, []string) {
	for _, workspace := range ordered {
		if workspace.ID == workspaceID {
			return []*taskmodels.Workspace{workspace}, []string{workspaceID}
		}
	}
	return nil, nil
}

func overviewParentIDs(tasks []*overviewTask) []string {
	var ids []string
	for _, t := range tasks {
		if t.row.OpenChildCount > 0 {
			ids = append(ids, t.row.ID)
		}
	}
	return ids
}

// aggregateWorkspaces selects the workspaces for the scope from the
// identity-scoped list and sorts them by name. The Office scope keeps only
// workspaces with an Office workflow; the reachable scope keeps all of them.
func aggregateWorkspaces(workspaces []*taskmodels.Workspace, scope string) ([]*taskmodels.Workspace, []string) {
	ids := make([]string, 0, len(workspaces))
	ordered := make([]*taskmodels.Workspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace == nil || workspace.ID == "" {
			continue
		}
		if scope != OverviewScopeReachable && workspace.OfficeWorkflowID == "" {
			continue
		}
		ordered = append(ordered, workspace)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Name != ordered[j].Name {
			return ordered[i].Name < ordered[j].Name
		}
		return ordered[i].ID < ordered[j].ID
	})
	for _, workspace := range ordered {
		ids = append(ids, workspace.ID)
	}
	return ordered, ids
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// processStartedAt reads the process start time the Office loop metrics
// publish; zero when unavailable.
func processStartedAt() time.Time {
	v := expvar.Get("office_loop_process_started_at")
	if v == nil {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339Nano, strings.Trim(v.String(), `"`))
	if err != nil {
		return time.Time{}
	}
	return at.UTC()
}
