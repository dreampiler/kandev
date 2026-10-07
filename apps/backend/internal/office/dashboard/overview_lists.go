package dashboard

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// Overview list filters for the per-workspace task list.
const (
	OverviewFilterProblems  = "problems"
	OverviewFilterActive    = "active"
	OverviewFilterSessions  = "sessions"
	OverviewFilterQueued    = "queued"
	OverviewFilterCompleted = "completed"
	OverviewFilterHold      = "hold"
	OverviewFilterAll       = "all"
)

// Overview list kinds for the cross-workspace running list.
const (
	OverviewKindTasks    = "tasks"
	OverviewKindSessions = "sessions"
	OverviewKindQueue    = "queue"
)

const (
	overviewTaskListDefaultLimit    = 50
	overviewRunningListDefaultLimit = 100
	overviewListMaxLimit            = 500
)

// OverviewListAll asks for every match instead of a page. It is a read intent,
// not a bigger ceiling: the response is bounded by the workspace's own matches
// for the filter, while every caller-supplied finite limit stays clamped at
// overviewListMaxLimit.
const OverviewListAll = -1

var (
	// ErrOverviewWorkspaceNotFound is returned when a list names a workspace
	// outside the caller's current overview.
	ErrOverviewWorkspaceNotFound = errors.New("workspace not found")
	// ErrOverviewBadRequest is returned for an unknown filter or kind.
	ErrOverviewBadRequest = errors.New("unknown overview filter or kind")
)

func (s *DashboardService) overviewListSnapshot(ctx context.Context) (*overviewSnapshot, error) {
	if s.overviewReader == nil {
		return nil, ErrWorkspaceAggregateUnavailable
	}
	return s.loadOverviewSnapshot(ctx)
}

// GetOverviewWorkspaceTasks returns one workspace's tasks for a filter, most
// severe first and, within a status, the longest-waiting first.
func (s *DashboardService) GetOverviewWorkspaceTasks(
	ctx context.Context, workspaceID, filter string, limit int,
) (*OverviewListResponse, error) {
	snap, err := s.overviewListSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := snap.names[workspaceID]; !ok {
		return nil, ErrOverviewWorkspaceNotFound
	}
	if filter == "" {
		filter = OverviewFilterProblems
	}
	var items []OverviewTaskItem
	switch filter {
	case OverviewFilterCompleted:
		items = completedTaskItems(snap, workspaceID)
	case OverviewFilterProblems, OverviewFilterActive, OverviewFilterSessions, OverviewFilterQueued,
		OverviewFilterHold, OverviewFilterAll:
		items = filteredTaskItems(snap, workspaceID, filter)
	default:
		return nil, ErrOverviewBadRequest
	}
	total := len(items)
	return &OverviewListResponse{
		Kind: OverviewKindTasks, Filter: filter, Total: total,
		Tasks: clipWorkspaceTaskItems(items, limit),
	}, nil
}

// clipWorkspaceTaskItems returns every match for an all read, and otherwise the
// page a bounded read asked for.
func clipWorkspaceTaskItems(items []OverviewTaskItem, limit int) []OverviewTaskItem {
	if limit == OverviewListAll {
		return items
	}
	return clip(items, listLimit(limit, overviewTaskListDefaultLimit))
}

// GetOverviewRunning returns the cross-workspace running tasks, running
// sessions, or queued messages.
func (s *DashboardService) GetOverviewRunning(ctx context.Context, kind string, limit int) (*OverviewListResponse, error) {
	snap, err := s.overviewListSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	limit = listLimit(limit, overviewRunningListDefaultLimit)
	resp := &OverviewListResponse{Kind: kind}
	switch kind {
	case OverviewKindTasks:
		items := filteredTaskItems(snap, "", OverviewFilterActive)
		resp.Total, resp.Tasks = len(items), clip(items, limit)
	case OverviewKindSessions:
		items := sessionItems(snap)
		resp.Total, resp.Sessions = len(items), clip(items, limit)
	case OverviewKindQueue:
		items := queueItems(snap)
		resp.Total = len(items)
		resp.Queue = clip(items, limit)
		if err := s.attachFirstLines(ctx, snap, resp.Queue); err != nil {
			return nil, err
		}
	default:
		return nil, ErrOverviewBadRequest
	}
	return resp, nil
}

func listLimit(limit, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	return min(limit, overviewListMaxLimit)
}

func clip[T any](items []T, limit int) []T {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func taskMatchesFilter(t *overviewTask, filter string) bool {
	switch filter {
	case OverviewFilterProblems:
		return isProblemStatus(t.status)
	case OverviewFilterActive:
		return t.isActive()
	case OverviewFilterSessions:
		return t.runningSession() != nil
	case OverviewFilterQueued:
		return t.queued > 0
	case OverviewFilterHold:
		return t.isHold()
	}
	return true
}

func filteredTaskItems(snap *overviewSnapshot, workspaceID, filter string) []OverviewTaskItem {
	var picked []*overviewTask
	for _, t := range snap.tasks {
		if (workspaceID == "" || t.row.WorkspaceID == workspaceID) && taskMatchesFilter(t, filter) {
			picked = append(picked, t)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		ri, rj := overviewStatusRank[picked[i].status], overviewStatusRank[picked[j].status]
		if ri != rj {
			return ri < rj
		}
		return picked[i].row.StepEnteredAt.Before(picked[j].row.StepEnteredAt)
	})
	items := make([]OverviewTaskItem, 0, len(picked))
	for _, t := range picked {
		items = append(items, taskItem(snap, t))
	}
	return items
}

func taskItem(snap *overviewSnapshot, t *overviewTask) OverviewTaskItem {
	item := OverviewTaskItem{
		TaskID: t.row.ID, Title: t.row.Title, StepName: t.row.StepName, State: t.row.State,
		WorkspaceID: t.row.WorkspaceID, WorkspaceName: snap.names[t.row.WorkspaceID],
		Status: t.status, Reason: t.reason, Failures24h: t.failures24h,
		LastOutputAt: timePtr(t.lastOutput), StepEnteredAt: t.row.StepEnteredAt, QueuedMessages: t.queued,
	}
	if t.shown != nil {
		item.SessionID = t.shown.ID
		item.SessionState = t.shown.State
		item.AgentProfileID = t.shown.AgentProfileID
		item.ModelName = snap.profileNames[t.shown.AgentProfileID]
	}
	item.Failure = taskFailure(snap, t)
	item.WaitingInputSessions = taskWaitingInputSessions(t)
	return item
}

// taskWaitingInputSessions counts the task's own sessions waiting on a person,
// so an expanded list answers that per row instead of leaving the reader to add
// up the scope's single waiting figure.
func taskWaitingInputSessions(t *overviewTask) int {
	waiting := 0
	for _, sess := range t.sessions {
		if sess.State == sessionStateWaitingForInput {
			waiting++
		}
	}
	return waiting
}

// taskFailure reports what followed the newest failed session of a task, so the
// row an operator is already looking at answers "and what happened to it?" in
// place rather than only in the events list. A task with no failed session
// carries none: there is no failure to follow up on.
func taskFailure(snap *overviewSnapshot, t *overviewTask) *OverviewFailure {
	var newest *sqlite.OverviewFailureFollowupRow
	for _, sess := range t.sessions {
		if sess.State != sessionStateFailed {
			continue
		}
		row := snap.followups[sess.ID]
		if row == nil {
			continue
		}
		if newest == nil || row.FailedAt.After(newest.FailedAt) {
			newest = row
		}
	}
	if newest == nil {
		return nil
	}
	return failureFor(newest, snap.now, snap.profileNames)
}

// sessionFailure reports what followed one session's failure. A row for a live
// session carries none, because a session that has not failed has nothing to
// follow up on.
func sessionFailure(snap *overviewSnapshot, sessionID string) *OverviewFailure {
	row := snap.followups[sessionID]
	if row == nil {
		return nil
	}
	return failureFor(row, snap.now, snap.profileNames)
}

func completedTaskItems(snap *overviewSnapshot, workspaceID string) []OverviewTaskItem {
	var items []OverviewTaskItem
	for _, row := range snap.completed {
		if row.WorkspaceID != workspaceID {
			continue
		}
		items = append(items, OverviewTaskItem{
			TaskID: row.ID, Title: row.Title, State: stateCompleted, WorkspaceID: row.WorkspaceID,
			WorkspaceName: snap.names[row.WorkspaceID], StepEnteredAt: row.CompletedAt,
			CompletedAt: &row.CompletedAt, Archived: !row.ArchivedAt.IsZero(),
		})
	}
	return items
}

func sessionItems(snap *overviewSnapshot) []OverviewSessionItem {
	taskWorkspace := map[string]string{}
	for _, t := range snap.tasks {
		taskWorkspace[t.row.ID] = t.row.WorkspaceID
	}
	items := make([]OverviewSessionItem, 0, len(snap.sessions))
	for _, sess := range snap.sessions {
		status, why := classifySession(sess, snap.now, defaultOverviewThresholds, snap.lastOutput)
		ws := taskWorkspace[sess.TaskID]
		items = append(items, OverviewSessionItem{
			SessionID: sess.ID, TaskID: sess.TaskID, TaskTitle: snap.taskTitles[sess.TaskID],
			WorkspaceID: ws, WorkspaceName: snap.names[ws], AgentProfileID: sess.AgentProfileID,
			ModelName: snap.profileNames[sess.AgentProfileID], SessionState: sess.State, Status: status, Reason: why,
			StartedAt: sess.StartedAt, LastOutputAt: timePtr(snap.lastOutput[sess.ID]),
			Failure: sessionFailure(snap, sess.ID),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := overviewStatusRank[items[i].Status], overviewStatusRank[items[j].Status]
		if ri != rj {
			return ri < rj
		}
		return items[i].StartedAt.Before(items[j].StartedAt)
	})
	return items
}

var overviewQueueRank = map[string]int{
	OverviewQueueUndeliverable: 0,
	OverviewQueueDelayed:       1,
	OverviewQueueWaiting:       2,
}

func queueItems(snap *overviewSnapshot) []OverviewQueueItem {
	items := make([]OverviewQueueItem, 0, len(snap.queues))
	for _, q := range snap.queues {
		status, why := classifyQueue(q, snap.now, defaultOverviewThresholds)
		items = append(items, OverviewQueueItem{
			SessionID: q.SessionID, TaskID: q.TaskID, TaskTitle: q.TaskTitle, WorkspaceID: q.WorkspaceID,
			WorkspaceName: snap.names[q.WorkspaceID], Status: status, Reason: why, Count: q.Count,
			OldestAt: q.Oldest, Sender: queueSender(q.QueuedBy), SessionState: q.SessionState,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := overviewQueueRank[items[i].Status], overviewQueueRank[items[j].Status]
		if ri != rj {
			return ri < rj
		}
		return items[i].OldestAt.Before(items[j].OldestAt)
	})
	return items
}

// attachFirstLines reads the head-of-queue message once per snapshot, only
// when the queued-messages list is actually requested.
func (s *DashboardService) attachFirstLines(ctx context.Context, snap *overviewSnapshot, items []OverviewQueueItem) error {
	snap.firstLinesOnce.Do(func() {
		ids := make([]string, 0, len(snap.queues))
		for _, q := range snap.queues {
			ids = append(ids, q.SessionID)
		}
		snap.firstLines, snap.firstLinesErr = s.overviewReader.FirstQueuedMessageBySession(context.WithoutCancel(ctx), ids)
	})
	if snap.firstLinesErr != nil {
		return snap.firstLinesErr
	}
	for i := range items {
		items[i].FirstLine = queueFirstLine(snap.firstLines[items[i].SessionID])
	}
	return nil
}

// getOverviewWorkspaceTasks serves GET /workspaces/aggregate/tasks.
func (h *Handler) getOverviewWorkspaceTasks(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	resp, err := h.svc.GetOverviewWorkspaceTasks(
		c.Request.Context(), c.Query("workspace_id"), c.Query("filter"), limit,
	)
	writeOverviewList(c, resp, err)
}

// getOverviewRunning serves GET /workspaces/aggregate/running.
func (h *Handler) getOverviewRunning(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	resp, err := h.svc.GetOverviewRunning(c.Request.Context(), c.Query("kind"), limit)
	writeOverviewList(c, resp, err)
}

func writeOverviewList(c *gin.Context, resp *OverviewListResponse, err error) {
	switch {
	case err == nil:
		c.JSON(http.StatusOK, resp)
	case errors.Is(err, ErrOverviewWorkspaceNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrOverviewBadRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrWorkspaceAggregateUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
