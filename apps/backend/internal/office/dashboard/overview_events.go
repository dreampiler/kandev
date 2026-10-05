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
	moves := foldStepMoves(group)
	first := group[0]
	last := group[len(group)-1]
	return OverviewEvent{
		Kind: overviewEventStepMove, At: last.OccurredAt, WorkspaceID: first.WorkspaceID,
		TaskID: first.TaskID, Title: first.Title,
		From: timePtr(first.OccurredAt), To: timePtr(last.OccurredAt),
		Moves: moves,
	}
}

// foldStepMoves collapses the movement a reader would act on into one entry per
// step the task actually settled in.
//
// Two shapes are folded, both because the task did not stay where the moves say
// it went. A move back to the step it just left changes nothing, so it merges
// into that step and advances its instant. A step the task entered and left
// again without settling — leaving A for B and returning straight to A — is a
// detour, so the detouring entry is dropped and the step it returned to carries
// the later instant. What survives is the path the task really took, which is
// what "Plan then back to Implement" and "Implement, Plan, Implement" both mean.
func foldStepMoves(group []*sqlite.OverviewStepTransitionRow) []OverviewStepMove {
	moves := make([]OverviewStepMove, 0, len(group))
	// stepIDs tracks the same steps in the same order, because a fold decides on
	// which step a move landed and the wire move itself names only the label.
	stepIDs := make([]string, 0, len(group))
	for _, row := range group {
		stepID := row.StepID
		switch {
		case len(moves) > 0 && stepID != "" && stepID == lastString(stepIDs):
			// Back to the step just left: same step, later instant.
			moves[len(moves)-1].At = row.OccurredAt
		case len(moves) > 1 && stepID != "" && stepID == secondLastString(stepIDs):
			// Entered a step and came straight back out: the entry it made is
			// the detour, so drop it and settle the earlier step here.
			moves = moves[:len(moves)-1]
			stepIDs = stepIDs[:len(stepIDs)-1]
			moves[len(moves)-1].At = row.OccurredAt
		default:
			moves = append(moves, OverviewStepMove{
				StepName: row.StepName, At: row.OccurredAt,
				Actor: row.ActorKind, Trigger: row.Trigger,
				// Stopped marks that arriving at this step starts nothing by
				// itself. It is read from the step's own configuration, so a
				// workflow that names its steps differently still answers
				// correctly.
				Stopped: !row.RunsOnEntry(),
			})
			stepIDs = append(stepIDs, stepID)
		}
	}
	return moves
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
