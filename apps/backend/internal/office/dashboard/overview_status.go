package dashboard

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kandev/kandev/internal/office/repository/sqlite"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// overviewThresholds holds every time limit the overview status rules use, in
// one place. Statuses are judged from task and session states, never from
// workflow step names, so the same rules hold for any workflow.
type overviewThresholds struct {
	// Stalled: a RUNNING session with no agent output for this long.
	NoOutput time.Duration
	// Delayed: time in the current step by task state.
	DwellInProgress time.Duration
	DwellReview     time.Duration
	DwellHold       time.Duration
	// Delayed: SCHEDULING (session ended, task not moved on) for this long.
	NotAdvancing time.Duration
	// Delayed: a STARTING session for this long.
	Starting time.Duration
	// Delayed: queued messages behind an idle/waiting session for this long.
	QueueIdle time.Duration
	// Delayed: queued messages behind a running turn for this long.
	QueueBusy time.Duration
	// Look-back window for failure counts, completions, and events.
	Window time.Duration
}

var defaultOverviewThresholds = overviewThresholds{
	NoOutput:        15 * time.Minute,
	DwellInProgress: 120 * time.Minute,
	DwellReview:     60 * time.Minute,
	DwellHold:       24 * time.Hour,
	NotAdvancing:    15 * time.Minute,
	Starting:        15 * time.Minute,
	QueueIdle:       10 * time.Minute,
	QueueBusy:       15 * time.Minute,
	Window:          24 * time.Hour,
}

// overviewErrorDetailMax bounds the verbatim agent error shown in a reason.
const overviewErrorDetailMax = 120

const (
	sessionStateCreated         = "CREATED"
	sessionStateStarting        = "STARTING"
	sessionStateRunning         = "RUNNING"
	sessionStateIdle            = "IDLE"
	sessionStateWaitingForInput = "WAITING_FOR_INPUT"
	sessionStateCompleted       = "COMPLETED"
	sessionStateFailed          = "FAILED"
	sessionStateCancelled       = "CANCELLED"

	taskStateScheduling      = "SCHEDULING"
	taskStateCreated         = "CREATED"
	taskStateWaitingForInput = "WAITING_FOR_INPUT"
	taskStateFailed          = "FAILED"
)

var overviewStatusRank = map[string]int{
	OverviewStatusError:   0,
	OverviewStatusStalled: 1,
	OverviewStatusDelayed: 2,
	OverviewStatusRunning: 3,
	OverviewStatusWaiting: 4,
	OverviewStatusBlocked: 5,
}

func isProblemStatus(status string) bool {
	return status == OverviewStatusError || status == OverviewStatusStalled || status == OverviewStatusDelayed
}

func isLiveSessionState(state string) bool {
	switch state {
	case sessionStateCreated, sessionStateStarting, sessionStateRunning, sessionStateIdle, sessionStateWaitingForInput:
		return true
	}
	return false
}

func isTerminalSessionState(state string) bool {
	return state == sessionStateCompleted || state == sessionStateFailed || state == sessionStateCancelled
}

func isActiveTaskState(state string) bool {
	return state == stateInProgress || state == stateInReview || state == taskStateScheduling
}

// overviewTask is one classified open task.
type overviewTask struct {
	row         *sqlite.OverviewTaskRow
	sessions    []*sqlite.OverviewSessionRow // newest first
	latest      *sqlite.OverviewSessionRow
	shown       *sqlite.OverviewSessionRow // the session the row links to
	lastOutput  time.Time
	queued      int
	oldestQueue time.Time
	failures24h int
	// automation is the step's own configuration: whether arriving at the
	// current step starts work by itself. It decides whether a task parked here
	// is being driven or is waiting for a person.
	automation wfmodels.StepAutomation
	status     string
	reason     *OverviewReason
}

// isActive reports whether the task counts as work in progress right now: an
// active task state with no unfinished blocker, on a step that starts something
// by itself, with a live turn and nothing owed to a person. A task waiting on a
// person or coordinating open children is none of those, and counts as neither
// in progress nor a problem.
func (t *overviewTask) isActive() bool {
	return isActiveTaskState(t.row.State) && t.row.OpenBlockers == 0 &&
		!t.awaitingStep() && !t.awaitingPerson()
}

// awaitingStep reports that the task sits on a bound step that starts nothing
// by itself and has nothing running, so its next move is a person's. A task
// with no workflow step has no step property to read and keeps the plain
// task-state answer.
func (t *overviewTask) awaitingStep() bool {
	return t.row.StepID != "" && !t.automation.RunsOnEntry() && t.runningSession() == nil
}

func (t *overviewTask) isHold() bool {
	return t.row.State == stateBlocked || t.row.OpenBlockers > 0
}

func minutesSince(now, at time.Time) int {
	if at.IsZero() || now.Before(at) {
		return 0
	}
	return int(now.Sub(at) / time.Minute)
}

func reason(code string, values map[string]any) *OverviewReason {
	return &OverviewReason{Code: code, Values: values}
}

// errorFirstLine returns the first non-empty line of an agent error, capped,
// so the UI can show the provider's own words without a stack dump.
func errorFirstLine(msg string) string {
	for _, line := range strings.Split(msg, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return truncateRunes(line, overviewErrorDetailMax)
	}
	return ""
}

func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit]) + "…"
}

// classify sets status and reason on the task. Checks run from most to least
// severe and the first match wins.
func (t *overviewTask) classify(now time.Time, th overviewThresholds, lastOutput map[string]time.Time) {
	t.prepare(now, th, lastOutput)
	if t.classifyError(lastOutput) {
		return
	}
	if t.classifyStalled(now, th, lastOutput) {
		return
	}
	if t.classifyDelayed(now, th, lastOutput) {
		return
	}
	t.classifySteady()
}

func (t *overviewTask) prepare(now time.Time, th overviewThresholds, lastOutput map[string]time.Time) {
	sort.SliceStable(t.sessions, func(i, j int) bool {
		return t.sessions[i].StartedAt.After(t.sessions[j].StartedAt)
	})
	if len(t.sessions) > 0 {
		t.latest = t.sessions[0]
	}
	t.shown = t.latest
	for _, s := range t.sessions {
		if s.State == sessionStateFailed && !s.StartedAt.Before(now.Add(-th.Window)) {
			t.failures24h++
		}
		if at := lastOutput[s.ID]; at.After(t.lastOutput) {
			t.lastOutput = at
		}
	}
	if live := t.runningSession(); live != nil {
		t.shown = live
	}
}

// runningSession is the newest RUNNING or STARTING session, if any.
func (t *overviewTask) runningSession() *sqlite.OverviewSessionRow {
	for _, s := range t.sessions {
		if s.State == sessionStateRunning || s.State == sessionStateStarting {
			return s
		}
	}
	return nil
}

// errorEligible reports whether a recent failure is the task's own current
// condition rather than something it is waiting past: an active task state, no
// unfinished blocker, nothing owed to a person, and a step that runs by itself.
// Between turns and open children are not part of this test, because a task
// that has run and stopped is exactly where a fresh failure is still an error.
func (t *overviewTask) errorEligible() bool {
	return isActiveTaskState(t.row.State) && t.row.OpenBlockers == 0 &&
		!t.awaitingStep() && !t.awaitingPerson()
}

func (t *overviewTask) classifyError(lastOutput map[string]time.Time) bool {
	if t.row.State == taskStateFailed {
		t.status, t.reason = OverviewStatusError, reason(reasonTaskFailed, nil)
		return true
	}
	latest := t.latest
	if latest != nil && latest.State == sessionStateFailed {
		t.status, t.reason = OverviewStatusError, reason(reasonSessionFailed, nil)
		t.reason.Detail = errorFirstLine(latest.ErrorMessage)
		t.shown = latest
		return true
	}
	if latest != nil && latest.ErrorMessage != "" && !lastOutput[latest.ID].After(latest.UpdatedAt) {
		t.status, t.reason = OverviewStatusError, reason(reasonSessionError, nil)
		t.reason.Detail = errorFirstLine(latest.ErrorMessage)
		t.shown = latest
		return true
	}
	if t.failures24h > 0 && t.runningSession() == nil && t.errorEligible() &&
		t.failedWithoutProgressSince(lastOutput) {
		t.status = OverviewStatusError
		t.reason = reason(reasonRecentFailures, map[string]any{"count": t.failures24h})
		return true
	}
	return false
}

// awaitingPerson reports that the task is waiting on a decision or an answer,
// so a failure from earlier is not its current condition.
func (t *overviewTask) awaitingPerson() bool {
	return t.row.State == taskStateWaitingForInput || t.hasSessionIn(sessionStateWaitingForInput)
}

// failedWithoutProgressSince reports that the newest thing that happened to the
// task is a failure: no session completed after it, no agent produced output
// after it, and the task has not entered a later step since. A failure the task
// has moved past is history, not its current condition, so it is not an error.
func (t *overviewTask) failedWithoutProgressSince(lastOutput map[string]time.Time) bool {
	failedAt := t.newestFailureAt()
	if failedAt.IsZero() {
		return false
	}
	for _, s := range t.sessions {
		if s.State == sessionStateCompleted && s.UpdatedAt.After(failedAt) {
			return false
		}
		if lastOutput[s.ID].After(failedAt) {
			return false
		}
	}
	return !t.row.StepEnteredAt.After(failedAt)
}

// newestFailureAt is the update time of the most recently failed session.
func (t *overviewTask) newestFailureAt() time.Time {
	var at time.Time
	for _, s := range t.sessions {
		if s.State == sessionStateFailed && s.UpdatedAt.After(at) {
			at = s.UpdatedAt
		}
	}
	return at
}

func (t *overviewTask) classifyStalled(now time.Time, th overviewThresholds, lastOutput map[string]time.Time) bool {
	for _, s := range t.sessions {
		if s.State != sessionStateRunning {
			continue
		}
		since := lastOutput[s.ID]
		if since.IsZero() || since.Before(s.StartedAt) {
			since = s.StartedAt
		}
		if now.Sub(since) >= th.NoOutput {
			t.status = OverviewStatusStalled
			t.reason = reason(reasonNoOutput, map[string]any{"minutes": minutesSince(now, since)})
			t.shown = s
			return true
		}
	}
	return false
}

func (t *overviewTask) classifyDelayed(now time.Time, th overviewThresholds, lastOutput map[string]time.Time) bool {
	if r := startingDelay(t.sessions, now, th, lastOutput); r != nil {
		t.status, t.reason = OverviewStatusDelayed, r
		return true
	}
	if t.row.State == taskStateScheduling && now.Sub(t.row.UpdatedAt) >= th.NotAdvancing {
		t.status = OverviewStatusDelayed
		t.reason = reason(reasonNotAdvancing, map[string]any{"minutes": minutesSince(now, t.row.UpdatedAt)})
		return true
	}
	if r := t.queueDelay(now, th); r != nil {
		t.status, t.reason = OverviewStatusDelayed, r
		return true
	}
	if limit := dwellLimit(t, th); limit > 0 && now.Sub(t.row.StepEnteredAt) >= limit {
		t.status = OverviewStatusDelayed
		t.reason = reason(reasonStepDwell, map[string]any{
			"minutes": minutesSince(now, t.row.StepEnteredAt),
			"step":    t.row.StepName,
		})
		return true
	}
	return false
}

// startingDelay flags a session stuck in STARTING, or one still marked
// STARTING although it is already producing output (a state anomaly).
func startingDelay(
	sessions []*sqlite.OverviewSessionRow, now time.Time, th overviewThresholds, lastOutput map[string]time.Time,
) *OverviewReason {
	for _, s := range sessions {
		if s.State != sessionStateStarting {
			continue
		}
		if lastOutput[s.ID].After(s.StartedAt) {
			return reason(reasonStartingWithOutput, nil)
		}
		if now.Sub(s.StartedAt) >= th.Starting {
			return reason(reasonStartingTooLong, map[string]any{"minutes": minutesSince(now, s.StartedAt)})
		}
	}
	return nil
}

func (t *overviewTask) queueDelay(now time.Time, th overviewThresholds) *OverviewReason {
	if t.queued == 0 || t.oldestQueue.IsZero() {
		return nil
	}
	waited := now.Sub(t.oldestQueue)
	if t.runningSession() != nil {
		if waited >= th.QueueBusy {
			return reason(reasonTurnNotFinishing, map[string]any{"minutes": minutesSince(now, t.oldestQueue)})
		}
		return nil
	}
	if waited >= th.QueueIdle {
		return reason(reasonQueueNotDelivered, map[string]any{"minutes": minutesSince(now, t.oldestQueue)})
	}
	return nil
}

// dwellLimit is how long the task may stay in its current step before that is
// a delay. A task waiting on a person is not delayed for waiting, and a parent
// with open children is read through them instead of its own clock, so neither
// limit below applies to them. Review and hold keep their own limits: those
// steps exist to be finished, not to be waited in.
func dwellLimit(t *overviewTask, th overviewThresholds) time.Duration {
	switch {
	case t.isHold():
		return th.DwellHold
	case t.row.State == stateInReview:
		return th.DwellReview
	case t.row.State == stateInProgress:
		return inProgressDwellLimit(t, th)
	}
	return 0
}

// inProgressDwellLimit is the limit for a working step. Time spent in one
// means something different depending on who owes the next move: a step that
// starts nothing by itself waits for a person, a task waiting for an answer
// waits for a person, and a parent with open children is read through them. A
// step that does start work by itself keeps the limit even when its last turn
// ended, because that is how a step that stopped advancing becomes visible.
func inProgressDwellLimit(t *overviewTask, th overviewThresholds) time.Duration {
	switch {
	case t.awaitingStep(), t.awaitingPerson(), t.row.OpenChildCount > 0:
		return 0
	}
	return th.DwellInProgress
}

func (t *overviewTask) classifySteady() {
	switch {
	case t.row.State == stateBlocked:
		t.status, t.reason = OverviewStatusBlocked, reason(reasonOnHold, nil)
	case t.row.OpenBlockers > 0:
		t.status = OverviewStatusWaiting
		t.reason = reason(reasonWaitingPrereq, map[string]any{"count": t.row.OpenBlockers})
	case t.runningSession() != nil:
		t.status, t.reason = OverviewStatusRunning, reason(reasonWorking, nil)
	case t.hasSessionIn(sessionStateWaitingForInput) || t.row.State == taskStateWaitingForInput:
		t.status, t.reason = OverviewStatusWaiting, reason(reasonWaitingInput, nil)
	case t.latest == nil:
		t.status, t.reason = OverviewStatusWaiting, reason(reasonNotStarted, nil)
	case isTerminalSessionState(t.latest.State):
		t.status, t.reason = OverviewStatusWaiting, reason(reasonSessionEnded, nil)
	default:
		t.status, t.reason = OverviewStatusWaiting, reason(reasonIdle, nil)
	}
}

func (t *overviewTask) hasSessionIn(state string) bool {
	for _, s := range t.sessions {
		if s.State == state {
			return true
		}
	}
	return false
}

// classifySession judges one RUNNING or STARTING session on its own.
func classifySession(
	s *sqlite.OverviewSessionRow, now time.Time, th overviewThresholds, lastOutput map[string]time.Time,
) (string, *OverviewReason) {
	out := lastOutput[s.ID]
	if s.ErrorMessage != "" && !out.After(s.UpdatedAt) {
		r := reason(reasonSessionError, nil)
		r.Detail = errorFirstLine(s.ErrorMessage)
		return OverviewStatusError, r
	}
	if r := startingDelay([]*sqlite.OverviewSessionRow{s}, now, th, lastOutput); r != nil {
		return OverviewStatusDelayed, r
	}
	if s.State == sessionStateRunning {
		since := out
		if since.IsZero() || since.Before(s.StartedAt) {
			since = s.StartedAt
		}
		if now.Sub(since) >= th.NoOutput {
			return OverviewStatusStalled, reason(reasonNoOutput, map[string]any{"minutes": minutesSince(now, since)})
		}
	}
	return OverviewStatusRunning, reason(reasonWorking, nil)
}

// classifyQueue judges a receiving session's queue: undeliverable when the
// session is gone or terminal, delayed when it has waited past its limit.
func classifyQueue(row *sqlite.OverviewQueueRow, now time.Time, th overviewThresholds) (string, *OverviewReason) {
	if row.SessionState == "" || isTerminalSessionState(row.SessionState) {
		return OverviewQueueUndeliverable, reason(reasonSessionEnded, map[string]any{"state": row.SessionState})
	}
	waited := now.Sub(row.Oldest)
	busy := row.SessionState == sessionStateRunning || row.SessionState == sessionStateStarting
	if busy && waited >= th.QueueBusy {
		return OverviewQueueDelayed, reason(reasonTurnNotFinishing, map[string]any{"minutes": minutesSince(now, row.Oldest)})
	}
	if !busy && waited >= th.QueueIdle {
		return OverviewQueueDelayed, reason(reasonQueueNotDelivered, map[string]any{"minutes": minutesSince(now, row.Oldest)})
	}
	return OverviewQueueWaiting, reason(reasonWorking, nil)
}

// queueSender maps queued_by onto a coarse sender class for display.
func queueSender(queuedBy string) string {
	switch queuedBy {
	case activityActorTypeAgent:
		return activityActorTypeAgent
	case "workflow", "mcp-move-task":
		return "workflow"
	case "server":
		return "system"
	}
	return "user"
}

// queueFirstLine strips <kandev-system>…</kandev-system> blocks and returns
// the first non-empty line, capped at 80 characters.
func queueFirstLine(content string) string {
	const open, closeTag = "<kandev-system>", "</kandev-system>"
	for {
		start := strings.Index(content, open)
		if start < 0 {
			break
		}
		end := strings.Index(content[start:], closeTag)
		if end < 0 {
			content = content[:start]
			break
		}
		content = content[:start] + content[start+end+len(closeTag):]
	}
	for _, line := range strings.Split(content, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return truncateRunes(line, 80)
		}
	}
	return ""
}

// errorKind reduces an error message to a groupable label: its first line,
// with anything after a long run of detail trimmed.
func errorKind(msg string) string {
	line := errorFirstLine(msg)
	if line == "" {
		return ""
	}
	return truncateRunes(line, 80)
}
