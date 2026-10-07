package dashboard

import (
	"context"
	"sort"
	"time"

	"github.com/kandev/kandev/internal/agent/agents"
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
	case t.isOnHold():
		m.BlockedTasks++
	case t.row.OpenBlockers > 0:
		m.BlockedByTasks++
	case t.isActive():
		m.ActiveTasks++
	default:
		// The remaining open tasks are the ones waiting on a person or between
		// turns. Counting them as active work is what made this screen and the
		// board disagree about the same task.
		m.WaitingTasks++
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
			// Concrete until the profile row names a dynamic agent; an
			// unreadable profile is not evidence of a dynamic profile.
			cards[profileID] = &OverviewModel{AgentProfileID: profileID, Kind: OverviewModelKindConcrete}
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
	s.accountIDsByProfile(ctx, snap)
	nameModelAccounts(cards, snap.accountIDs)
	snap.resp.Models = sortedModels(cards, errorKinds)
	return s.assembleBlockedAccounts(ctx, snap, ids)
}

// nameModelAccounts attaches the provider account of every model card, so a
// client can group the cards by the account they run on instead of scattering
// one account's models across the list. A dynamic profile routes through other
// profiles and is not itself an account, so it keeps no account.
func nameModelAccounts(cards map[string]*OverviewModel, accountIDs map[string]string) {
	for _, c := range cards {
		if c.Kind == OverviewModelKindDynamic {
			continue
		}
		c.AccountID = accountIDs[c.AgentProfileID]
	}
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
		name := profileDisplayName(p)
		snap.profileNames[p.ID] = name
		if c := cards[p.ID]; c != nil {
			c.AgentID, c.AgentName, c.Name = p.AgentID, p.AgentName, name
			c.Kind = modelKind(p.AgentID)
		}
	}
	return nil
}

// profileDisplayName is the one name a profile has across the overview: the
// agent display name qualifies the profile name, so the same profile reads the
// same in a model card and in a blocked circuit.
func profileDisplayName(p *sqlite.OverviewProfileRow) string {
	if p.DisplayName != "" && p.DisplayName != p.Name {
		return p.DisplayName + " · " + p.Name
	}
	return p.Name
}

// modelKind separates a dynamic profile, which routes one logical session
// through ordered concrete profiles, from a concrete profile that is one model
// on its own. The agent identity is the same discriminator the profile editor
// uses, so a profile cannot look different in two places.
func modelKind(agentID string) string {
	if agentID == agents.DynamicAgentID {
		return OverviewModelKindDynamic
	}
	return OverviewModelKindConcrete
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

// loadAnswerableQuestions reads the answerable question bundles of this pass's
// live sessions once and records which tasks owe an answer, so classification can
// treat a task waiting on the owner as waiting rather than as delayed and the
// needs-human list renders from the same read instead of asking again.
func (s *DashboardService) loadAnswerableQuestions(ctx context.Context, snap *overviewSnapshot) error {
	var liveIDs []string
	for _, t := range snap.tasks {
		for _, sess := range t.sessions {
			if isLiveSessionState(sess.State) {
				liveIDs = append(liveIDs, sess.ID)
			}
		}
	}
	if s.questionLister == nil || len(liveIDs) == 0 {
		return nil
	}
	questions, err := s.questionLister.ListAnswerableClarificationsForSessions(ctx, liveIDs)
	if err != nil {
		return err
	}
	snap.questions = questions
	owning := make(map[string]bool, len(questions))
	for _, q := range questions {
		owning[q.TaskID] = true
	}
	for _, t := range snap.tasks {
		t.awaitingOwner = owning[t.row.ID]
	}
	return nil
}

// assembleNeedsHuman lists answerable questions on live sessions and pending
// approvals.
func (s *DashboardService) assembleNeedsHuman(ctx context.Context, snap *overviewSnapshot, ids []string) error {
	var items []OverviewHumanItem
	taskWorkspace := map[string]string{}
	for _, t := range snap.tasks {
		taskWorkspace[t.row.ID] = t.row.WorkspaceID
	}
	for _, q := range snap.questions {
		ws := taskWorkspace[q.TaskID]
		items = append(items, OverviewHumanItem{
			Kind: "question", ID: q.PendingID, WorkspaceID: ws, WorkspaceName: snap.names[ws],
			TaskID: q.TaskID, TaskTitle: snap.taskTitles[q.TaskID], SessionID: q.SessionID,
			Count: 1, CreatedAt: q.CreatedAt,
			// questionKey is the bundle's question identity, so the same
			// question asked again after a task restart collapses into one
			// row. A bundle without one keeps its own row rather than
			// joining an unrelated question.
			questionKey: q.QuestionID,
		})
	}
	approvals, err := s.overviewReader.ListOverviewPendingApprovals(ctx, ids, overviewPendingApprovalLimit)
	if err != nil {
		return err
	}
	for _, a := range approvals {
		items = append(items, OverviewHumanItem{
			Kind: "approval", ID: a.ID, WorkspaceID: a.WorkspaceID, WorkspaceName: snap.names[a.WorkspaceID],
			ApprovalType: a.Type, Count: 1, CreatedAt: a.CreatedAt, questionKey: a.Type,
		})
	}
	snap.resp.NeedsHuman = collapseHumanItems(items)
	return nil
}

// collapseHumanItems merges the occurrences that ask the same thing of the same
// task into one row, keeping the newest occurrence as the one to answer and the
// number of occurrences as its count. Occurrences without a question identity
// stay separate rows, because merging them would merge different questions. The
// result is oldest first, so the longest-waiting item is read first.
func collapseHumanItems(items []OverviewHumanItem) []OverviewHumanItem {
	type group struct {
		key  string
		item *OverviewHumanItem
	}
	byKey := map[string]*OverviewHumanItem{}
	order := make([]group, 0, len(items))
	for i := range items {
		item := items[i]
		key := item.questionKey
		if key == "" {
			separate := item
			order = append(order, group{item: &separate})
			continue
		}
		groupKey := item.Kind + "|" + item.WorkspaceID + "|" + item.TaskID + "|" + key
		if existing, ok := byKey[groupKey]; ok {
			existing.Count++
			if item.CreatedAt.After(existing.CreatedAt) {
				existing.ID, existing.SessionID = item.ID, item.SessionID
				existing.CreatedAt = item.CreatedAt
			}
			continue
		}
		merged := item
		merged.questionKey = ""
		byKey[groupKey] = &merged
		order = append(order, group{key: groupKey, item: &merged})
	}
	out := make([]OverviewHumanItem, 0, len(order))
	for _, entry := range order {
		out = append(out, *entry.item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func assembleSystem(snap *overviewSnapshot, capacity SessionCapacityReading, capacityKnown bool, thresholds overviewThresholds) {
	sys := &OverviewSystem{StartedAt: timePtr(processStartedAt())}
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
	// The card counts what the list shows: one entry per distinct thing to
	// answer, not the occurrences behind it and not the raw approval total.
	// Counting the list keeps the two from disagreeing when either is capped.
	for _, item := range snap.resp.NeedsHuman {
		if item.Kind == "question" {
			sys.NeedsHuman++
		}
	}
	for _, q := range snap.queues {
		if status, _ := classifyQueue(q, snap.now, thresholds); status == OverviewQueueUndeliverable {
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
		earliestUnblockAt(sys, snap.now, acct.RetryAt)
	}
	circuits := 0
	for _, circuit := range snap.resp.BlockedCircuits {
		// A circuit the router already recovered from is reported for its strike
		// history, not as a block, so it counts neither here nor in the clear-at
		// instant the card shows.
		if !circuit.Blocking {
			continue
		}
		circuits++
		earliestUnblockAt(sys, snap.now, circuit.Until)
	}
	if snap.circuitsAvailable {
		total := sys.BlockedAccounts + circuits
		sys.BlockedAccountsTotal = &total
	}
	sys.ProblemThresholds = overviewThresholdsWire(thresholds)
	assembleSessionCapacity(snap, sys, capacity, capacityKnown)
	snap.resp.System = sys
}

// assembleSessionCapacity reports the running-session card's denominators from
// the admission reading taken on this pass, together with each lane's own share
// of the scope's waiting-input sessions. A reading that did not arrive leaves
// the capacities absent, so the client shows the scope's own running count
// without a limit rather than a denominator nothing measured.
func assembleSessionCapacity(snap *overviewSnapshot, sys *OverviewSystem, capacity SessionCapacityReading, known bool) {
	if !known {
		return
	}
	lanes := &OverviewSessionLanes{
		GeneralLimit: capacity.GeneralLimit,
		ControlLimit: capacity.ControlLimit,
	}
	if capacity.PopulationKnown {
		general, control := capacity.GeneralUsed, capacity.ControlUsed
		lanes.GeneralRunningSessions = &general
		lanes.ControlRunningSessions = &control
		generalWaiting, controlWaiting := laneWaitingInput(snap, capacity.ControlProfileIDs)
		lanes.GeneralWaitingInputSessions = &generalWaiting
		lanes.ControlWaitingInputSessions = &controlWaiting
	}
	sys.SessionLanes = lanes
}

// earliestUnblockAt keeps the soonest clear instant across both block sources.
func earliestUnblockAt(sys *OverviewSystem, now time.Time, at *time.Time) {
	if at == nil || !at.After(now) {
		return
	}
	if sys.EarliestUnblockAt == nil || at.Before(*sys.EarliestUnblockAt) {
		sys.EarliestUnblockAt = at
	}
}

// overviewThresholdsWire reports the limits the status rules actually applied,
// so the client explains the same numbers the classification used.
func overviewThresholdsWire(th overviewThresholds) *OverviewThresholds {
	return &OverviewThresholds{
		NoOutputMinutes:        int(th.NoOutput / time.Minute),
		StartingMinutes:        int(th.Starting / time.Minute),
		NotAdvancingMinutes:    int(th.NotAdvancing / time.Minute),
		QueueIdleMinutes:       int(th.QueueIdle / time.Minute),
		QueueBusyMinutes:       int(th.QueueBusy / time.Minute),
		DwellInProgressMinutes: int(th.DwellInProgress / time.Minute),
		DwellReviewMinutes:     int(th.DwellReview / time.Minute),
		WindowHours:            int(th.Window / time.Hour),
	}
}

// assembleEvents merges the last-24-hours sources, newest first. Each kind
// contributes up to overviewEventKindLimit rows and the merge is bounded
// separately, so the screen can filter down to one kind before it caps what it
// renders.
func assembleEvents(
	snap *overviewSnapshot,
	automation []*sqlite.OverviewAutomationTaskRow,
	created []*sqlite.OverviewCreatedTaskRow,
	extra *overviewExtraEvents,
	since time.Time,
) []OverviewEvent {
	var events []OverviewEvent
	if started := processStartedAt(); !started.IsZero() && started.After(since) {
		events = append(events, OverviewEvent{
			Kind: overviewEventServerStarted, At: started, Version: snap.buildVersion,
		})
	}
	events = append(events, createdTaskEvents(created)...)
	for _, row := range automation {
		events = append(events, OverviewEvent{
			Kind: overviewEventAutomationRun, At: row.CreatedAt, WorkspaceID: row.WorkspaceID, TaskID: row.ID, Title: row.Title,
			AutomationID: row.AutomationID,
		})
	}
	if extra != nil {
		events = append(events, extraEvents(extra, snap.profileNames)...)
	}
	for i, row := range snap.completed {
		if i >= overviewEventKindLimit {
			break
		}
		events = append(events, OverviewEvent{
			Kind: overviewEventTaskCompleted, At: row.CompletedAt, WorkspaceID: row.WorkspaceID, TaskID: row.ID, Title: row.Title,
		})
	}
	events = append(events, failedSessionEvents(snap, since)...)
	sortEventsNewest(events)
	if len(events) > overviewEventMergeLimit {
		events = events[:overviewEventMergeLimit]
	}
	return events
}

// extraEvents renders the additional last-24-hours sources into events, keeping
// the per-kind bound each one carries. The profile names are the ones this pass
// already resolved, so a model block names its subject the way the
// blocked-circuits card does.
func extraEvents(extra *overviewExtraEvents, profileNames map[string]string) []OverviewEvent {
	var events []OverviewEvent
	events = append(events, modelBlockEvents(extra.blocks, profileNames)...)
	events = append(events, mergedPREvents(extra.mergedPRs)...)
	events = append(events, automationFailureEvents(extra.automationFailures)...)
	events = append(events, decisionEvents(extra.decisions)...)
	return append(events, stepMoveEvents(extra.stepMoves)...)
}

// createdTaskEvents reports tasks a person or the control plane created, with
// the parent task's title as the supporting detail.
func createdTaskEvents(created []*sqlite.OverviewCreatedTaskRow) []OverviewEvent {
	events := make([]OverviewEvent, 0, min(len(created), overviewEventKindLimit))
	for i, row := range created {
		if i >= overviewEventKindLimit {
			break
		}
		events = append(events, OverviewEvent{
			Kind: overviewEventTaskCreated, At: row.CreatedAt, WorkspaceID: row.WorkspaceID,
			TaskID: row.ID, Title: row.Title, Detail: row.ParentTitle,
		})
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
			event := OverviewEvent{
				Kind: overviewEventSessionFailed, At: sess.UpdatedAt, WorkspaceID: t.row.WorkspaceID,
				TaskID: t.row.ID, SessionID: sess.ID, Title: t.row.Title, Detail: errorFirstLine(sess.ErrorMessage),
			}
			// The follow-up is read on this same pass, so the line describing
			// what happened after the failure advances whenever the screen does
			// rather than staying as it was when the failure first appeared.
			if followup := snap.followups[sess.ID]; followup != nil {
				event.Failure = failureFor(followup, snap.now, snap.profileNames)
			}
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
	if len(events) > overviewEventKindLimit {
		events = events[:overviewEventKindLimit]
	}
	return events
}
