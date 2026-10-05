package dashboard

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
)

// The last-24-hours event kinds assembled from the dedicated sources, and the
// failure follow-up that rides on a failed session's row.
//
// Every builder here sends codes and values, never a sentence. The client
// composes each phrase in the viewer's language from the same code vocabulary
// the other overview sections already use, so no server-side English leaks into
// a translated screen and no two callers can disagree about what a code means.

// overviewExtraEvents is the last-24-hours material the original merge does not
// cover, read once per pass and handed to the merge as a unit so the ordering
// and the per-kind bounds stay in one place.
type overviewExtraEvents struct {
	blocks             []*sqlite.OverviewModelBlockRow
	mergedPRs          []*sqlite.OverviewMergedPRRow
	automationFailures []*sqlite.OverviewAutomationFailureRow
	decisions          []*sqlite.OverviewDecisionRow
	stepMoves          []*sqlite.OverviewStepTransitionRow
}

// loadProfileNames adds the display names of the supplied profiles to the
// snapshot. It merges rather than replaces, so a caller naming a profile for a
// failure follow-up does not discard the names the model cards resolved. A read
// that fails leaves the names it could not resolve absent: an unnamed model
// reports as unknown rather than failing the whole overview.
func (s *DashboardService) loadProfileNames(
	ctx context.Context, snap *overviewSnapshot, profileIDs []string,
) error {
	if len(profileIDs) == 0 {
		return nil
	}
	sort.Strings(profileIDs)
	profiles, err := s.overviewReader.ListOverviewProfiles(ctx, profileIDs)
	if err != nil {
		return err
	}
	if snap.profileNames == nil {
		snap.profileNames = map[string]string{}
	}
	for _, p := range profiles {
		snap.profileNames[p.ID] = profileDisplayName(p)
	}
	return nil
}

// nameBlockSubjects resolves the agent profiles the circuit rows name, so a
// block event reports the same subject the blocked-circuits card does instead
// of the key only. A read that fails, or a profile that no longer resolves,
// leaves that event's subject unnamed, which the client reports as
// unidentifiable rather than falling back to the raw key.
func (s *DashboardService) nameBlockSubjects(
	ctx context.Context, snap *overviewSnapshot, rows []*sqlite.OverviewModelBlockRow,
) {
	missing := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if row == nil || !row.IsCircuit() {
			continue
		}
		id := circuitIdentityOf(row.Resource).profileID
		if id == "" || seen[id] || snap.profileNames[id] != "" {
			continue
		}
		seen[id] = true
		missing = append(missing, id)
	}
	_ = s.loadProfileNames(ctx, snap, missing)
}

// modelBlockEvents reports a model account or provider limit being blocked, and
// the same resource lifting again. A resource that has already lifted by this
// read is reported as unblocked at its own clear time rather than as still
// blocking: the row is the only record of when, so nothing here guesses.
//
// A circuit row travels as the fields the blocked-circuits card carries, so one
// naming rule phrases both: the client reads a profile name, a model, and a
// reason code rather than a key. A provider limit keeps the provider id as its
// title, because that id is the provider's own name.
func modelBlockEvents(
	rows []*sqlite.OverviewModelBlockRow, profileNames map[string]string,
) []OverviewEvent {
	events := make([]OverviewEvent, 0, len(rows))
	for i, row := range rows {
		if i >= overviewEventKindLimit {
			break
		}
		kind := overviewEventModelBlocked
		at := row.BlockedAt
		if row.Cleared {
			kind = overviewEventModelUnblocked
			if !row.ClearedAt.IsZero() {
				at = row.ClearedAt
			}
		}
		event := OverviewEvent{
			Kind: kind, At: at, ClearsAt: timePtr(row.ClearedAt), Reason: row.Code,
		}
		if row.IsCircuit() {
			scope, scopeValue := splitCircuitKey(row.Resource)
			identity := circuitIdentityOf(row.Resource)
			event.Scope = scope
			event.ScopeValue = scopeValue
			event.ProfileID = identity.profileID
			event.ProfileName = profileNames[identity.profileID]
			event.ModelName = identity.modelName
		} else {
			event.Title = row.Resource
		}
		events = append(events, event)
	}
	return events
}

// mergedPREvents reports the change requests that were merged inside the window.
func mergedPREvents(rows []*sqlite.OverviewMergedPRRow) []OverviewEvent {
	events := make([]OverviewEvent, 0, len(rows))
	for i, row := range rows {
		if i >= overviewEventKindLimit {
			break
		}
		events = append(events, OverviewEvent{
			Kind: overviewEventPRMerged, At: row.MergedAt, WorkspaceID: row.WorkspaceID,
			TaskID: row.TaskID, Title: row.Title,
			PullRequest: &OverviewPullRequest{Owner: row.Owner, Repo: row.Repo, Number: row.Number},
		})
	}
	return events
}

// automationFailureEvents reports the automation runs that failed, with the
// run's own error text kept out of translation as Detail.
func automationFailureEvents(rows []*sqlite.OverviewAutomationFailureRow) []OverviewEvent {
	events := make([]OverviewEvent, 0, len(rows))
	for i, row := range rows {
		if i >= overviewEventKindLimit {
			break
		}
		events = append(events, OverviewEvent{
			Kind: overviewEventAutomationFail, At: row.FailedAt, WorkspaceID: row.WorkspaceID,
			AutomationID: row.AutomationID, TaskID: row.TaskID, Title: row.Title,
			Detail: errorFirstLine(row.ErrorMessage),
		})
	}
	return events
}

// decisionEvents reports each thing a person was asked to decide, carrying the
// answer instant only once one exists. An open question therefore reports no
// answer rather than an invented one.
func decisionEvents(rows []*sqlite.OverviewDecisionRow) []OverviewEvent {
	events := make([]OverviewEvent, 0, len(rows))
	for i, row := range rows {
		if i >= overviewEventKindLimit {
			break
		}
		events = append(events, OverviewEvent{
			Kind: overviewEventOwnerDecision, At: row.AskedAt, WorkspaceID: row.WorkspaceID,
			Title: row.Type, DecidedAt: timePtr(row.DecidedAt),
		})
	}
	return events
}

// stepMoveEvents folds each task's committed step changes into one row. A task
// that walked through several steps reads as a single run rather than as a pile
// of near-identical rows, and a task that left a step and came straight back
// contributes one step instead of two, because the detour left no trace a reader
// would act on.
//
// Rows arrive grouped by task and ordered oldest first, so grouping is a single
// pass. The actor and trigger travel as the codes the ledger recorded: who moved
// a task is reported when the ledger says who, and no actor is invented for a row
// that records none.
func stepMoveEvents(rows []*sqlite.OverviewStepTransitionRow) []OverviewEvent {
	var events []OverviewEvent
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].TaskID == rows[start].TaskID {
			end++
		}
		events = append(events, stepMoveEvent(rows[start:end]))
		start = end
	}
	return events
}

// stepMoveEvent builds the one row that stands for a task's run of moves.
func stepMoveEvent(group []*sqlite.OverviewStepTransitionRow) OverviewEvent {
	moves, sentBack, reopened := foldStepMoves(group)
	first := group[0]
	last := group[len(group)-1]
	return OverviewEvent{
		Kind: overviewEventStepMove, At: last.OccurredAt, WorkspaceID: first.WorkspaceID,
		TaskID: first.TaskID, Title: first.Title,
		From: timePtr(first.OccurredAt), To: timePtr(last.OccurredAt),
		Moves: moves, MoveTotal: len(group),
		SentBack: sentBack, Reopened: reopened,
		Held: len(moves) > 0 && moves[len(moves)-1].Stopped,
	}
}

// foldStepMoves collapses the movement a reader would act on into one entry per
// step the task actually settled in, and reports how many moves were sent back
// and how many opened finished work again.
//
// Two shapes are folded, both because repeating them on the row tells a reader
// nothing a count does not. A move back to the step it just left changes nothing,
// so it merges into that step and advances its instant. A step the task entered
// and left again without settling — leaving A for B and returning straight to A
// — is a detour, so the detouring entry is dropped and the step it returned to
// carries the later instant. What survives is the path the task really took,
// which is what "Plan then back to Implement" and "Implement, Plan, Implement"
// both mean. A folded move is not lost: it is counted on the entry that absorbed
// it, so an entry can stand for several movements between the same two steps.
//
// Classification is read per recorded row, not per surviving entry, because the
// movements worth noticing are usually the ones the fold absorbs: Review ->
// Implement is exactly the detour case, and it is a send-back. For the same
// reason the set of steps this run has entered is kept apart from the folded
// entries: a fold drops the steps a run only passed through, and a step it
// dropped is still a step the task had been in.
func foldStepMoves(group []*sqlite.OverviewStepTransitionRow) (moves []OverviewStepMove, sentBack, reopened int) {
	moves = make([]OverviewStepMove, 0, len(group))
	// stepIDs tracks the surviving entries in order, because a fold decides on
	// which step a move landed and the wire move itself names only the label.
	stepIDs := make([]string, 0, len(group))
	// visited is every step this run entered, recorded as each row is read and
	// never rewritten by a fold.
	visited := make(map[string]bool, len(group))
	for _, row := range group {
		stepID := row.StepID
		sent, reopenedNow := classifyRow(row, visited)
		if sent {
			sentBack++
		}
		if reopenedNow {
			reopened++
		}
		if stepID != "" {
			visited[stepID] = true
		}
		switch {
		case len(moves) > 0 && stepID != "" && stepID == lastString(stepIDs):
			// Back to the step just left: same step, later instant.
			absorb(&moves[len(moves)-1], row, sent, reopenedNow)
		case len(moves) > 1 && stepID != "" && stepID == secondLastString(stepIDs):
			// Entered a step and came straight back out: the entry it made is
			// the detour, so drop it and settle the earlier step here.
			moves = moves[:len(moves)-1]
			stepIDs = stepIDs[:len(stepIDs)-1]
			absorb(&moves[len(moves)-1], row, sent, reopenedNow)
		default:
			moves = append(moves, newStepMove(row, sent, reopenedNow))
			stepIDs = append(stepIDs, stepID)
		}
	}
	return moves, sentBack, reopened
}

// absorb folds one recorded move into the entry that already describes where the
// task settled, counting it and keeping whatever it revealed.
func absorb(move *OverviewStepMove, row *sqlite.OverviewStepTransitionRow, sentBack, reopened bool) {
	move.At = row.OccurredAt
	move.Repeat++
	move.SentBack = move.SentBack || sentBack
	move.Reopened = move.Reopened || reopened
	if row.ActorKind != "" {
		move.Actor = row.ActorKind
	}
	if row.Trigger != "" {
		move.Trigger = row.Trigger
	}
}

// classifyRow reads what one recorded move was, from the ledger alone: arriving
// at a step this run had already entered is work being sent back, and arriving
// from a step that starts nothing on entry is finished or held work being opened
// again. Reopened wins when both hold, because a task moved out of a finished
// step is not being sent back to anything. A move whose source or destination
// identity the ledger does not record settles nothing.
//
// visited is every step the run entered before this row, which is not the same
// as the steps that survived the fold: a fold drops a step a run only passed
// through, and returning to such a step is still a return.
func classifyRow(
	row *sqlite.OverviewStepTransitionRow, visited map[string]bool,
) (sentBack, reopened bool) {
	if row.FromStepID == "" {
		return false, false
	}
	if !row.RunsOnEntryFrom() {
		return false, true
	}
	return row.StepID != "" && visited[row.StepID], false
}

// newStepMove builds one surviving entry. Stopped marks that arriving at this
// step starts nothing by itself, read from the step's own configuration, so a
// workflow that names its steps differently still answers correctly.
func newStepMove(row *sqlite.OverviewStepTransitionRow, sentBack, reopened bool) OverviewStepMove {
	return OverviewStepMove{
		FromStepName: row.FromStepName, StepName: row.StepName, At: row.OccurredAt,
		Actor: row.ActorKind, Trigger: row.Trigger,
		Stopped: !row.RunsOnEntry(), SentBack: sentBack, Reopened: reopened, Repeat: 1,
	}
}

// lastString and secondLastString read the tail of the step-id list without the
// caller repeating the bounds checks.
func lastString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func secondLastString(values []string) string {
	if len(values) < 2 {
		return ""
	}
	return values[len(values)-2]
}

// failureFor reports what followed one failed session. The three follow-ups are
// independent, so each is reported on its own and the server's own verdict that
// none of them found anything is what the screen highlights. Elapsed figures are
// measured against this read, so they advance whenever the screen does.
func failureFor(
	row *sqlite.OverviewFailureFollowupRow, now time.Time, modelNames map[string]string,
) *OverviewFailure {
	if row == nil {
		return nil
	}
	out := &OverviewFailure{
		TaskState:        row.TaskState,
		RouteReason:      row.NextRouteReason,
		RouteAttempts:    row.RouteAttempts,
		FailedAgoMinutes: overviewElapsedMinutes(row.FailedAt, now),
	}
	if row.NextSessionID != "" {
		out.NextSession = &OverviewFollowupSession{
			SessionID: row.NextSessionID, State: row.NextState, StartedAt: row.NextStarted,
			ModelName:         modelNames[row.NextProfileID],
			StartedAgoMinutes: overviewElapsedMinutes(row.NextStarted, now),
			LaterSessions:     row.LaterSessions,
		}
	}
	out.HasNoAction = out.NextSession == nil && row.RouteAttempts == 0 &&
		row.NextRouteReason == "" && !isTerminalTaskState(row.TaskState)
	return out
}

// isTerminalTaskState reports whether a task state means the task finished
// rather than waiting. A task in any other state is treated as still needing
// something, so an open task with no follow-up reads as untouched.
func isTerminalTaskState(state string) bool {
	switch strings.ToUpper(state) {
	case "COMPLETED", "CANCELLED":
		return true
	}
	return false
}

// overviewElapsedMinutes is whole minutes from `at` to `now`, floored at zero so
// a clock skew between the writer and this read cannot report a negative age.
func overviewElapsedMinutes(at, now time.Time) int {
	if at.IsZero() {
		return 0
	}
	elapsed := now.Sub(at)
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed / time.Minute)
}

// sortEventsNewest orders a merged list newest first. It is stable so rows that
// share an instant keep the order their sources produced them in.
func sortEventsNewest(events []OverviewEvent) {
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.After(events[j].At) })
}
