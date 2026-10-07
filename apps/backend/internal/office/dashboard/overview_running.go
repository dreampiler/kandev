package dashboard

import "sort"

// overviewRunningTaskLimit bounds how many running tasks one workspace card
// names. The card's summary line answers "what is moving here", so it stops at
// a few and reports the remainder as a count rather than growing without bound.
const overviewRunningTaskLimit = 5

// assembleRunningTasks attaches each workspace's running tasks to its card, most
// severe first and then the one that has waited longest in its step, which is the
// order the rest of the card already uses. It reads the tasks this pass already
// classified, so the card's summary line and its figures come from one read
// rather than a second list that only loads once the card is expanded.
func assembleRunningTasks(snap *overviewSnapshot) {
	byWorkspace := map[string][]*overviewTask{}
	for _, t := range snap.tasks {
		if t.runningSession() == nil {
			continue
		}
		byWorkspace[t.row.WorkspaceID] = append(byWorkspace[t.row.WorkspaceID], t)
	}
	for i := range snap.resp.Workspaces {
		entry := &snap.resp.Workspaces[i]
		tasks := byWorkspace[entry.WorkspaceID]
		sort.SliceStable(tasks, func(a, b int) bool { return worseTask(tasks[a], tasks[b]) })
		entry.Running, entry.RunningTruncated = runningTaskItems(snap, tasks)
	}
}

// runningTaskItems renders the bounded list a card names. A task beyond the limit
// is counted rather than dropped, so the card can say how many more exist.
func runningTaskItems(snap *overviewSnapshot, tasks []*overviewTask) ([]OverviewRunningTask, int) {
	if len(tasks) == 0 {
		return nil, 0
	}
	truncated := max(len(tasks)-overviewRunningTaskLimit, 0)
	if truncated > 0 {
		tasks = tasks[:overviewRunningTaskLimit]
	}
	items := make([]OverviewRunningTask, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, runningTaskItem(snap, t))
	}
	return items, truncated
}

// runningTaskItem is one running task with that task's own session counts, so a
// line says how much of the task is working and how much of it waits on a
// person. A task with no executing session has no line at all.
func runningTaskItem(snap *overviewSnapshot, t *overviewTask) OverviewRunningTask {
	running, waiting := 0, 0
	for _, s := range t.sessions {
		switch s.State {
		case sessionStateRunning, sessionStateStarting:
			running++
		case sessionStateWaitingForInput:
			waiting++
		}
	}
	return OverviewRunningTask{
		TaskID:               t.row.ID,
		Title:                t.row.Title,
		Status:               t.status,
		StepName:             t.row.StepName,
		AgentName:            snap.profileNames[t.runningSession().AgentProfileID],
		RunningSessions:      running,
		WaitingInputSessions: waiting,
	}
}

// laneWaitingInput splits this scope's waiting-input sessions by admission lane,
// using the same control profile set the capacity reading classified its
// populations with. The two counts therefore add up to the scope's own waiting
// figure, and an install with no control lane puts every waiting session in the
// general lane exactly as classOfLocked does.
func laneWaitingInput(snap *overviewSnapshot, controlProfiles []string) (general, control int) {
	controlSet := map[string]struct{}{}
	for _, id := range controlProfiles {
		controlSet[id] = struct{}{}
	}
	for _, t := range snap.tasks {
		for _, s := range t.sessions {
			if s.State != sessionStateWaitingForInput {
				continue
			}
			if _, isControl := controlSet[s.AgentProfileID]; isControl {
				control++
				continue
			}
			general++
		}
	}
	return general, control
}
