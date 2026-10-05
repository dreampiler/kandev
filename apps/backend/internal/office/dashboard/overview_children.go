package dashboard

// A parent task is a coordination surface, not a worker: while its children are
// open, what the parent owes the next move is its children. These rules keep a
// parent row from inventing a problem of its own while work below it is still
// running, and they keep every child visible on its own row rather than
// restated through the parent.

// applyChildStatus reads a parent that has open children through them. A parent
// whose own verdict came only from its dwell clock or from a failure it has
// already moved past is reported as waiting on its open children instead. A
// parent whose verdict came from its own live session, its own task state, or
// its own failure keeps it, and a child's own status is never changed here.
func applyChildStatus(tasks []*overviewTask) {
	for _, t := range tasks {
		if t.row.OpenChildCount <= 0 || !childDerivedReason(t.reason) {
			continue
		}
		t.status = OverviewStatusWaiting
		t.reason = reason(reasonWaitingPrereq, map[string]any{"count": t.row.OpenChildCount})
	}
}

// childDerivedReason reports whether the parent's verdict is about the parent
// having waited rather than about the parent itself being broken.
func childDerivedReason(r *OverviewReason) bool {
	if r == nil {
		return false
	}
	return r.Code == reasonStepDwell || r.Code == reasonRecentFailures
}
