package dashboard

import (
	"context"
	"sort"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// assembleWorkspaceMetrics fills each workspace entry's metrics and parent
// groups from the classified tasks.
func assembleWorkspaceMetrics(snap *overviewSnapshot, completed24h, childCounts map[string]int) {
	metrics := map[string]*OverviewWorkspaceMetrics{}
	parents := map[string][]OverviewParentTask{}
	worst := map[string]*overviewTask{}
	for i := range snap.resp.Workspaces {
		id := snap.resp.Workspaces[i].WorkspaceID
		metrics[id] = &OverviewWorkspaceMetrics{Completed24h: completed24h[id]}
	}
	for _, t := range snap.tasks {
		m := metrics[t.row.WorkspaceID]
		if m == nil {
			continue
		}
		addTaskMetrics(m, t, snap.lastOutput)
		if isProblemStatus(t.status) && worseTask(t, worst[t.row.WorkspaceID]) {
			worst[t.row.WorkspaceID] = t
		}
		if t.row.OpenChildCount > 0 {
			parents[t.row.WorkspaceID] = append(parents[t.row.WorkspaceID], OverviewParentTask{
				TaskID: t.row.ID, Title: t.row.Title, Status: t.status,
				Children: max(childCounts[t.row.ID], t.row.OpenChildCount), OpenChildren: t.row.OpenChildCount,
			})
		}
	}
	for _, q := range snap.queues {
		if m := metrics[q.WorkspaceID]; m != nil {
			m.QueuedMessages += q.Count
		}
	}
	for i := range snap.resp.Workspaces {
		entry := &snap.resp.Workspaces[i]
		m := metrics[entry.WorkspaceID]
		if t := worst[entry.WorkspaceID]; t != nil {
			m.TopWarning = &OverviewWarning{TaskID: t.row.ID, TaskTitle: t.row.Title, Status: t.status, Reason: t.reason}
		}
		m.Status = workspaceStatus(m)
		entry.Metrics = m
		entry.Parents = topParents(parents[entry.WorkspaceID])
	}
}

func addTaskMetrics(m *OverviewWorkspaceMetrics, t *overviewTask, lastOutput map[string]time.Time) {
	m.OpenTasks++
	switch {
	case t.row.State == stateBlocked:
		m.BlockedTasks++
	case t.row.OpenBlockers > 0:
		m.WaitingTasks++
	case t.isActive():
		m.ActiveTasks++
	}
	switch t.status {
	case OverviewStatusError:
		m.Problems.Error++
	case OverviewStatusStalled:
		m.Problems.Stalled++
	case OverviewStatusDelayed:
		m.Problems.Delayed++
	}
	for _, s := range t.sessions {
		switch s.State {
		case sessionStateRunning, sessionStateStarting:
			m.RunningSessions++
		case sessionStateWaitingForInput:
			m.WaitingInputSessions++
		}
		if at := lastOutput[s.ID]; isLiveSessionState(s.State) && (m.LastOutputAt == nil || at.After(*m.LastOutputAt)) {
			if !at.IsZero() {
				m.LastOutputAt = timePtr(at)
				m.LastOutputTaskID = t.row.ID
			}
		}
	}
}

// worseTask reports whether candidate outranks current: higher severity
// first, then the one that has waited longer.
func worseTask(candidate, current *overviewTask) bool {
	if current == nil {
		return true
	}
	if overviewStatusRank[candidate.status] != overviewStatusRank[current.status] {
		return overviewStatusRank[candidate.status] < overviewStatusRank[current.status]
	}
	return candidate.row.StepEnteredAt.Before(current.row.StepEnteredAt)
}

func workspaceStatus(m *OverviewWorkspaceMetrics) string {
	switch {
	case m.Problems.Error > 0:
		return OverviewStatusError
	case m.Problems.Stalled > 0:
		return OverviewStatusStalled
	case m.Problems.Delayed > 0:
		return OverviewStatusDelayed
	case m.RunningSessions > 0 || m.ActiveTasks > 0:
		return OverviewStatusRunning
	case m.BlockedTasks > 0 && m.BlockedTasks == m.OpenTasks:
		return OverviewStatusBlocked
	}
	return OverviewStatusWaiting
}

func topParents(parents []OverviewParentTask) []OverviewParentTask {
	sort.SliceStable(parents, func(i, j int) bool {
		if parents[i].OpenChildren != parents[j].OpenChildren {
			return parents[i].OpenChildren > parents[j].OpenChildren
		}
		return parents[i].Title < parents[j].Title
	})
	if len(parents) > overviewParentLimit {
		parents = parents[:overviewParentLimit]
	}
	return parents
}

// assembleModels builds the per-profile model cards and blocked accounts.
func (s *DashboardService) assembleModels(
	ctx context.Context, snap *overviewSnapshot, ids []string, since time.Time,
) error {
	sessions, err := s.overviewReader.ListOverviewProfileSessions(ctx, ids, since)
	if err != nil {
		return err
	}
	cards := map[string]*OverviewModel{}
	errorKinds := map[string]map[string]int{}
	card := func(profileID string) *OverviewModel {
		if cards[profileID] == nil {
			cards[profileID] = &OverviewModel{AgentProfileID: profileID}
			errorKinds[profileID] = map[string]int{}
		}
		return cards[profileID]
	}
	for _, row := range sessions {
		if row.AgentProfileID == "" {
			continue
		}
		c := card(row.AgentProfileID)
		c.Sessions24h++
		if row.State == sessionStateFailed {
			c.Failed24h++
		}
		if kind := errorKind(row.ErrorMessage); kind != "" {
			errorKinds[row.AgentProfileID][kind]++
		}
	}
	for _, sess := range snap.sessions {
		if sess.AgentProfileID != "" {
			card(sess.AgentProfileID).Running++
		}
	}
	if err := s.nameModels(ctx, snap, cards); err != nil {
		return err
	}
	snap.resp.Models = sortedModels(cards, errorKinds)
	return s.assembleBlockedAccounts(ctx, snap, ids)
}

func (s *DashboardService) nameModels(ctx context.Context, snap *overviewSnapshot, cards map[string]*OverviewModel) error {
	profileIDs := make([]string, 0, len(cards))
	for id := range cards {
		profileIDs = append(profileIDs, id)
	}
	sort.Strings(profileIDs)
	profiles, err := s.overviewReader.ListOverviewProfiles(ctx, profileIDs)
	if err != nil {
		return err
	}
	snap.profileNames = map[string]string{}
	for _, p := range profiles {
		name := p.Name
		if p.DisplayName != "" && p.DisplayName != p.Name {
			name = p.DisplayName + " · " + p.Name
		}
		snap.profileNames[p.ID] = name
		if c := cards[p.ID]; c != nil {
			c.AgentID, c.AgentName, c.Name = p.AgentID, p.AgentName, name
		}
	}
	return nil
}

func sortedModels(cards map[string]*OverviewModel, errorKinds map[string]map[string]int) []OverviewModel {
	out := make([]OverviewModel, 0, len(cards))
	for id, c := range cards {
		for kind, count := range errorKinds[id] {
			c.Errors = append(c.Errors, OverviewErrorKind{Kind: kind, Count: count})
		}
		sort.Slice(c.Errors, func(i, j int) bool {
			if c.Errors[i].Count != c.Errors[j].Count {
				return c.Errors[i].Count > c.Errors[j].Count
			}
			return c.Errors[i].Kind < c.Errors[j].Kind
		})
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Running != out[j].Running {
			return out[i].Running > out[j].Running
		}
		if out[i].Sessions24h != out[j].Sessions24h {
			return out[i].Sessions24h > out[j].Sessions24h
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *DashboardService) assembleBlockedAccounts(ctx context.Context, snap *overviewSnapshot, ids []string) error {
	rows, err := s.overviewReader.ListOverviewBlockedProviders(ctx, ids)
	if err != nil {
		return err
	}
	for _, row := range rows {
		snap.resp.BlockedAccounts = append(snap.resp.BlockedAccounts, OverviewBlockedAccount{
			WorkspaceID: row.WorkspaceID, ProviderID: row.ProviderID, Scope: row.Scope,
			ScopeValue: row.ScopeValue, State: row.State, ErrorCode: row.ErrorCode, RetryAt: timePtr(row.RetryAt),
		})
	}
	return nil
}

// assembleNeedsHuman lists answerable questions on live sessions and pending
// approvals.
func (s *DashboardService) assembleNeedsHuman(ctx context.Context, snap *overviewSnapshot, ids []string) error {
	var items []OverviewHumanItem
	taskWorkspace := map[string]string{}
	var liveIDs []string
	for _, t := range snap.tasks {
		taskWorkspace[t.row.ID] = t.row.WorkspaceID
		for _, sess := range t.sessions {
			if isLiveSessionState(sess.State) {
				liveIDs = append(liveIDs, sess.ID)
			}
		}
	}
	if s.questionLister != nil && len(liveIDs) > 0 {
		questions, err := s.questionLister.ListAnswerableClarificationsForSessions(ctx, liveIDs)
		if err != nil {
			return err
		}
		for _, q := range questions {
			ws := taskWorkspace[q.TaskID]
			items = append(items, OverviewHumanItem{
				Kind: "question", ID: q.PendingID, WorkspaceID: ws, WorkspaceName: snap.names[ws],
				TaskID: q.TaskID, TaskTitle: snap.taskTitles[q.TaskID], SessionID: q.SessionID, CreatedAt: q.CreatedAt,
			})
		}
	}
	approvals, err := s.overviewReader.ListOverviewPendingApprovals(ctx, ids, overviewPendingApprovalLimit)
	if err != nil {
		return err
	}
	for _, a := range approvals {
		items = append(items, OverviewHumanItem{
			Kind: "approval", ID: a.ID, WorkspaceID: a.WorkspaceID, WorkspaceName: snap.names[a.WorkspaceID],
			ApprovalType: a.Type, CreatedAt: a.CreatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	snap.resp.NeedsHuman = items
	return nil
}

func assembleSystem(snap *overviewSnapshot, sessionLimit int) {
	sys := &OverviewSystem{SessionLimit: sessionLimit, StartedAt: timePtr(processStartedAt())}
	for _, entry := range snap.resp.Workspaces {
		if m := entry.Metrics; m != nil {
			sys.ActiveTasks += m.ActiveTasks
			sys.RunningSessions += m.RunningSessions
			sys.WaitingInputSessions += m.WaitingInputSessions
			sys.QueuedMessages += m.QueuedMessages
			sys.Problems += m.Problems.Error + m.Problems.Stalled + m.Problems.Delayed
		}
		sys.NeedsHuman += entry.PendingApprovals
	}
	for _, item := range snap.resp.NeedsHuman {
		if item.Kind == "question" {
			sys.NeedsHuman++
		}
	}
	for _, q := range snap.queues {
		if status, _ := classifyQueue(q, snap.now, defaultOverviewThresholds); status == OverviewQueueUndeliverable {
			sys.UndeliverableMessages += q.Count
		}
	}
	seen := map[string]bool{}
	for _, acct := range snap.resp.BlockedAccounts {
		key := acct.ProviderID + "|" + acct.Scope + "|" + acct.ScopeValue
		if !seen[key] {
			seen[key] = true
			sys.BlockedAccounts++
		}
		if acct.RetryAt != nil && acct.RetryAt.After(snap.now) &&
			(sys.EarliestUnblockAt == nil || acct.RetryAt.Before(*sys.EarliestUnblockAt)) {
			sys.EarliestUnblockAt = acct.RetryAt
		}
	}
	snap.resp.System = sys
}

// assembleEvents merges the last-24-hours sources, newest first.
func assembleEvents(
	snap *overviewSnapshot, automation []*sqlite.OverviewAutomationTaskRow, since time.Time,
) []OverviewEvent {
	var events []OverviewEvent
	if started := processStartedAt(); !started.IsZero() && started.After(since) {
		events = append(events, OverviewEvent{Kind: overviewEventServerStarted, At: started})
	}
	for i, row := range snap.completed {
		if i >= overviewEventSourceLimit {
			break
		}
		events = append(events, OverviewEvent{
			Kind: overviewEventTaskCompleted, At: row.UpdatedAt, WorkspaceID: row.WorkspaceID, TaskID: row.ID, Title: row.Title,
		})
	}
	events = append(events, failedSessionEvents(snap, since)...)
	for _, row := range automation {
		events = append(events, OverviewEvent{
			Kind: overviewEventAutomationRun, At: row.CreatedAt, WorkspaceID: row.WorkspaceID, TaskID: row.ID, Title: row.Title,
		})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
	if len(events) > overviewEventLimit {
		events = events[:overviewEventLimit]
	}
	return events
}

func failedSessionEvents(snap *overviewSnapshot, since time.Time) []OverviewEvent {
	var events []OverviewEvent
	for _, t := range snap.tasks {
		for _, sess := range t.sessions {
			if sess.State != sessionStateFailed || sess.UpdatedAt.Before(since) {
				continue
			}
			events = append(events, OverviewEvent{
				Kind: overviewEventSessionFailed, At: sess.UpdatedAt, WorkspaceID: t.row.WorkspaceID,
				TaskID: t.row.ID, SessionID: sess.ID, Title: t.row.Title, Detail: errorFirstLine(sess.ErrorMessage),
			})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
	if len(events) > overviewEventSourceLimit {
		events = events[:overviewEventSourceLimit]
	}
	return events
}
