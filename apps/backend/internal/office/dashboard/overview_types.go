package dashboard

import (
	"strconv"
	"strings"
	"time"
)

// The periods the project-statistics block may cover. The owner asked for a
// period selection, so the block follows the caller's choice while every other
// overview section keeps its own fixed 24-hour window.
const (
	OverviewStatsWindow24h = 24
	OverviewStatsWindow7d  = 168
	OverviewStatsWindow30d = 720
)

// defaultOverviewStatsWindowHours is the period a request that names none gets,
// so the default screen is unchanged.
const defaultOverviewStatsWindowHours = OverviewStatsWindow24h

var overviewStatsWindowHours = []int{OverviewStatsWindow24h, OverviewStatsWindow7d, OverviewStatsWindow30d}

// ParseOverviewStatsWindow validates the `window_hours` request value. An empty
// value is the default; anything outside the supported set is rejected rather
// than clamped, so a caller never believes it selected a period the screen will
// not use.
func ParseOverviewStatsWindow(raw string) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultOverviewStatsWindowHours, true
	}
	hours, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, false
	}
	for _, allowed := range overviewStatsWindowHours {
		if hours == allowed {
			return hours, true
		}
	}
	return 0, false
}

// Overview wire types for the multi-workspace aggregate. Reasons travel as a
// code plus values so the UI composes the sentence in the viewer's language;
// Detail carries an agent's own error text verbatim and is never translated.

// Overview task statuses, ordered by overviewStatusRank.
const (
	OverviewStatusError   = "error"
	OverviewStatusStalled = "stalled"
	OverviewStatusDelayed = "delayed"
	OverviewStatusRunning = "running"
	OverviewStatusWaiting = "waiting"
	OverviewStatusBlocked = "blocked"
)

// Queued-message delivery statuses.
const (
	OverviewQueueUndeliverable = "undeliverable"
	OverviewQueueDelayed       = "delayed"
	OverviewQueueWaiting       = "waiting"
)

// Overview reason codes. The web client owns the sentence for each code.
const (
	reasonSessionFailed      = "session_failed"
	reasonSessionError       = "session_error"
	reasonTaskFailed         = "task_failed"
	reasonRecentFailures     = "recent_failures"
	reasonNoOutput           = "no_output"
	reasonStartingTooLong    = "starting_too_long"
	reasonStartingWithOutput = "starting_with_output"
	reasonNotAdvancing       = "not_advancing"
	reasonQueueNotDelivered  = "queue_not_delivered"
	reasonTurnNotFinishing   = "turn_not_finishing"
	reasonStepDwell          = "step_dwell"
	reasonWorking            = "working"
	reasonWaitingInput       = "waiting_input"
	reasonWaitingPrereq      = "waiting_prereq"
	reasonNotStarted         = "not_started"
	reasonIdle               = "idle"
	reasonOnHold             = "on_hold"
	reasonSessionEnded       = "session_ended"
)

// Overview event kinds for the last-24-hours section. Every kind here is one
// the kind chips can filter on; the web client owns the label for each code.
const (
	overviewEventServerStarted  = "server_started"
	overviewEventTaskCreated    = "task_created"
	overviewEventTaskCompleted  = "task_completed"
	overviewEventSessionFailed  = "session_failed"
	overviewEventAutomationRun  = "automation_run"
	overviewEventModelBlocked   = "model_blocked"
	overviewEventModelUnblocked = "model_unblocked"
	overviewEventPRMerged       = "pr_merged"
	overviewEventAutomationFail = "automation_failed"
	overviewEventOwnerDecision  = "owner_decision"
	overviewEventStepMove       = "step_move"
)

// OverviewReason is a machine-readable explanation of a status.
type OverviewReason struct {
	Code   string         `json:"code"`
	Values map[string]any `json:"values,omitempty"`
	Detail string         `json:"detail,omitempty"`
}

// Overview system card row.
type OverviewSystem struct {
	StartedAt            *time.Time `json:"started_at,omitempty"`
	ActiveTasks          int        `json:"active_tasks"`
	RunningSessions      int        `json:"running_sessions"`
	WaitingInputSessions int        `json:"waiting_input_sessions"`
	// SessionLanes carries the configured session limits and the population each
	// lane holds, read from the admission controller on this pass. It is absent
	// when that reading did not arrive, which is different from a reading of
	// zero: the client then shows the scope's own running count with no
	// denominator instead of a limit nothing measured. A limit of zero means the
	// lane is unlimited (general) or not configured (control).
	SessionLanes          *OverviewSessionLanes `json:"session_lanes,omitempty"`
	QueuedMessages        int                   `json:"queued_messages"`
	UndeliverableMessages int                   `json:"undeliverable_messages"`
	NeedsHuman            int                   `json:"needs_human"`
	BlockedAccounts       int                   `json:"blocked_accounts"`
	// BlockedAccountsTotal counts provider-health blocks plus open dynamic
	// circuits, so the client never presents a provider-health-only number as
	// the whole picture. It is absent while the circuits source is unavailable.
	BlockedAccountsTotal *int                `json:"blocked_accounts_total,omitempty"`
	EarliestUnblockAt    *time.Time          `json:"earliest_unblock_at,omitempty"`
	Problems             int                 `json:"problems"`
	ProblemThresholds    *OverviewThresholds `json:"problem_thresholds,omitempty"`
}

// OverviewSessionLanes is the instance's session admission state: the general
// lane and the control lane, each with the limit it is admitted against and the
// population it holds. The two populations are absent together when the count
// could not be read, so the limits still show while nothing claims a count.
type OverviewSessionLanes struct {
	GeneralLimit           int  `json:"general_limit"`
	ControlLimit           int  `json:"control_limit"`
	GeneralRunningSessions *int `json:"general_running_sessions,omitempty"`
	ControlRunningSessions *int `json:"control_running_sessions,omitempty"`
	// GeneralWaitingInputSessions and ControlWaitingInputSessions split the same
	// scope's waiting-input count by lane, so a lane's tile reports that lane's
	// own sessions waiting on a person rather than the instance total. They are
	// absent together when the populations are, and a measured zero is reported
	// as a zero rather than dropped.
	GeneralWaitingInputSessions *int `json:"general_waiting_input_sessions,omitempty"`
	ControlWaitingInputSessions *int `json:"control_waiting_input_sessions,omitempty"`
}

// OverviewThresholds are the time limits the status rules actually applied, in
// minutes, so the client can explain what counts as an error, a stall, or a
// delay without restating the rules. The client phrases the codes; it never
// invents a limit the backend did not use.
type OverviewThresholds struct {
	NoOutputMinutes        int `json:"no_output_minutes"`
	StartingMinutes        int `json:"starting_minutes"`
	NotAdvancingMinutes    int `json:"not_advancing_minutes"`
	QueueIdleMinutes       int `json:"queue_idle_minutes"`
	QueueBusyMinutes       int `json:"queue_busy_minutes"`
	DwellInProgressMinutes int `json:"dwell_in_progress_minutes"`
	DwellReviewMinutes     int `json:"dwell_review_minutes"`
	WindowHours            int `json:"window_hours"`
}

// OverviewProblemCounts splits a workspace's problem tasks by status.
type OverviewProblemCounts struct {
	Error   int `json:"error"`
	Stalled int `json:"stalled"`
	Delayed int `json:"delayed"`
}

// OverviewWarning names the single most severe problem task of a workspace.
type OverviewWarning struct {
	TaskID    string          `json:"task_id"`
	TaskTitle string          `json:"task_title"`
	Status    string          `json:"status"`
	Reason    *OverviewReason `json:"reason,omitempty"`
}

// OverviewRunningTask is one workspace task that has a session executing right
// now. It is what the project card's summary line reads, because the card
// answers "what is moving in this project" from the same read that counted it
// rather than from a second list that only loads once the card is expanded.
type OverviewRunningTask struct {
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	StepName  string `json:"step_name"`
	AgentName string `json:"agent_name,omitempty"`
	// RunningSessions and WaitingInputSessions are that task's own sessions, so
	// one line says how much of it is working and how much of it waits on a
	// person.
	RunningSessions      int `json:"running_sessions"`
	WaitingInputSessions int `json:"waiting_input_sessions"`
}

// OverviewWorkspaceMetrics are the six project-card metrics plus problems.
type OverviewWorkspaceMetrics struct {
	Status               string     `json:"status"`
	ActiveTasks          int        `json:"active_tasks"`
	RunningSessions      int        `json:"running_sessions"`
	WaitingInputSessions int        `json:"waiting_input_sessions"`
	LastOutputAt         *time.Time `json:"last_output_at,omitempty"`
	LastOutputTaskID     string     `json:"last_output_task_id,omitempty"`
	QueuedMessages       int        `json:"queued_messages"`
	Completed24h         int        `json:"completed_24h"`
	OpenTasks            int        `json:"open_tasks"`
	WaitingTasks         int        `json:"waiting_tasks"`
	BlockedTasks         int        `json:"blocked_tasks"`
	// BlockedByTasks is the open tasks waiting on an unfinished predecessor.
	// It is its own figure rather than part of WaitingTasks because a task held
	// on a hold step, a task waiting for a predecessor, and a task waiting on a
	// person are three different reasons a task is not moving, and the card
	// shows all three.
	BlockedByTasks int                   `json:"blocked_by_tasks"`
	Problems       OverviewProblemCounts `json:"problems"`
	TopWarning     *OverviewWarning      `json:"top_warning,omitempty"`
	// Activity is the period statistics block. It is present whenever the
	// overview reader answered, even for a workspace with no work, because the
	// block answers "how much happened here in the period" rather than "what is
	// running now".
	Activity *OverviewWorkspaceActivity `json:"activity,omitempty"`
}

// OverviewParentTask is an open task that groups child tasks.
type OverviewParentTask struct {
	TaskID       string `json:"task_id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Children     int    `json:"children"`
	OpenChildren int    `json:"open_children"`
}

// OverviewErrorKind counts one kind of session error.
type OverviewErrorKind struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// OverviewFailureBucket counts the failed sessions of one period that fell in
// one reason bucket. Code is a stable identifier the client phrases, never the
// agent's own words.
type OverviewFailureBucket struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// OverviewWorkspaceActivity is the project-statistics block for one workspace:
// five period totals plus the failure breakdown the owner asked to be able to
// expand. WindowHours is the period actually applied, so the client labels the
// numbers with the backend's answer rather than its own guess.
type OverviewWorkspaceActivity struct {
	WindowHours     int                     `json:"window_hours"`
	Completed       int                     `json:"completed"`
	SessionsStarted int                     `json:"sessions_started"`
	SessionsFailed  int                     `json:"sessions_failed"`
	AgentTurns      int                     `json:"agent_turns"`
	StepMoves       int                     `json:"step_moves"`
	FailureBuckets  []OverviewFailureBucket `json:"failure_buckets,omitempty"`
	// FailureSamples carries the agent's own first-line errors behind the
	// buckets, the same verbatim text the model card already shows, so the
	// breakdown answers "why" with something more specific than the bucket.
	FailureSamples []OverviewErrorKind `json:"failure_samples,omitempty"`
}

// Overview model kinds. A dynamic profile routes one logical session through
// ordered concrete profiles; a concrete profile is one model on its own.
const (
	OverviewModelKindConcrete = "concrete"
	OverviewModelKindDynamic  = "dynamic"
)

// OverviewModel is one agent profile in the models section.
type OverviewModel struct {
	AgentProfileID string `json:"agent_profile_id"`
	AgentID        string `json:"agent_id"`
	AgentName      string `json:"agent_name"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	// AccountID is the provider account this profile authenticates with, so a
	// client can group the models of one account. It is empty when the account
	// is not identifiable, and for a dynamic profile, which routes through
	// concrete profiles rather than being an account of its own.
	AccountID   string              `json:"account_id,omitempty"`
	Sessions24h int                 `json:"sessions_24h"`
	Running     int                 `json:"running"`
	Failed24h   int                 `json:"failed_24h"`
	Errors      []OverviewErrorKind `json:"errors"`
}

// OverviewBlockedCircuit is one dynamic-routing resource circuit that is not
// plainly healthy. It is the dynamic counterpart of a provider-health block: the
// router is avoiding this resource until Until, and the state is unknown rather
// than healthy when the circuits source is unavailable.
//
// A circuit that already recovered, or whose suspension window has passed, is
// reported too, because its retained strike history is why the durable source
// still lists it. Blocking separates the two, so the current-block list and the
// blocked count never carry a resource that is no longer blocked.
type OverviewBlockedCircuit struct {
	ResourceKey string     `json:"resource_key"`
	Scope       string     `json:"scope"`
	ScopeValue  string     `json:"scope_value"`
	State       string     `json:"state"`
	Code        string     `json:"code,omitempty"`
	Until       *time.Time `json:"until,omitempty"`
	Strikes     int        `json:"strikes"`
	// Blocking is true while this resource is still unavailable at the instant
	// the snapshot was taken: a suspension whose Until is still ahead, or one
	// with no clear instant. An expired suspension is false even when the
	// durable state still reads open, because no selection has yet claimed its
	// probe and the block window itself is over.
	Blocking bool `json:"blocking"`
	// ProfileID is the agent profile this circuit was recorded for, when the
	// circuit key names one. It is empty for a binding fingerprint that no
	// profile claims, which is not an error and not a guessable identity.
	ProfileID   string `json:"profile_id,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
	// ModelName is the concrete model of a model-scoped circuit, and empty for
	// a circuit that covers a whole profile or account.
	ModelName string `json:"model_name,omitempty"`
}

// OverviewBlockedAccount is one provider account the router is avoiding.
type OverviewBlockedAccount struct {
	WorkspaceID string     `json:"workspace_id"`
	ProviderID  string     `json:"provider_id"`
	Scope       string     `json:"scope"`
	ScopeValue  string     `json:"scope_value"`
	State       string     `json:"state"`
	ErrorCode   string     `json:"error_code,omitempty"`
	RetryAt     *time.Time `json:"retry_at,omitempty"`
}

// OverviewEvent is one entry in the last-24-hours section.
type OverviewEvent struct {
	Kind         string    `json:"kind"`
	At           time.Time `json:"at"`
	WorkspaceID  string    `json:"workspace_id,omitempty"`
	AutomationID string    `json:"automation_id,omitempty"`
	TaskID       string    `json:"task_id,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	Title        string    `json:"title,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	// Version is the running build reported on a server-start row. It is the
	// version the binary injected at link time, never one derived here, so a
	// build that reports nothing shows no version rather than a guess.
	Version string `json:"version,omitempty"`
	// ClearsAt is when a block is expected to lift. A resource that never
	// named a clear time leaves it absent.
	ClearsAt *time.Time `json:"clears_at,omitempty"`
	// A model block's subject, carried as the same fields the blocked-circuits
	// card carries so one naming rule phrases both. Scope is the circuit's scope
	// ("profile", "credential", or "model"), ScopeValue its fingerprint, and the
	// profile and model the row resolved to. All are absent for a provider
	// limit, which is already named by the provider it carries.
	Scope       string `json:"scope,omitempty"`
	ScopeValue  string `json:"scope_value,omitempty"`
	ProfileID   string `json:"profile_id,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
	ModelName   string `json:"model_name,omitempty"`
	// Reason is the circuit's classification code. The client phrases it with
	// the same vocabulary the blocked-circuits card uses, so the code itself is
	// never the only thing on the row.
	Reason string `json:"reason,omitempty"`
	// PullRequest identifies a merged change request: repository and number,
	// kept as fields so the client never parses them out of prose.
	PullRequest *OverviewPullRequest `json:"pull_request,omitempty"`
	// DecidedAt is when a person answered an owner decision. Absent while the
	// question is still open, which is the state the needs-a-person list
	// already reports separately.
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	// From and To bound a task's run of moves: when the run began and when it
	// settled. The moves carry their own instants, so these two are the span a
	// reader compares rather than something derived twice.
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`
	// Moves is a run of consecutive workflow-step changes for one task,
	// oldest first. Empty for every kind but a step move.
	Moves []OverviewStepMove `json:"moves,omitempty"`
	// MoveTotal counts every committed transition in the run before repeated and
	// detouring steps were folded out of Moves, so the summary a reader sees
	// accounts for the whole run rather than for what survived the fold.
	MoveTotal int `json:"move_total,omitempty"`
	// SentBack and Reopened count the moves worth noticing, and Held reports
	// that the run ended parked on a step that starts nothing. They are read
	// from the ledger's own rows here rather than by the client, because what
	// counts as going backwards depends on steps this run already passed
	// through.
	SentBack int  `json:"sent_back,omitempty"`
	Reopened int  `json:"reopened,omitempty"`
	Held     bool `json:"held,omitempty"`
	// Failure is what happened after a session failed, read on this same pass
	// so it advances whenever the screen does. Absent when the failure is the
	// newest thing known about the task.
	Failure *OverviewFailure `json:"failure,omitempty"`
}

// OverviewPullRequest names a merged change request.
type OverviewPullRequest struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// OverviewStepMove is one step change inside a task's run of moves. StepName
// is empty for a step whose row is gone or unnamed, and Stopped reports that
// arriving here starts nothing by itself, read from the step's own
// configuration rather than from its name.
type OverviewStepMove struct {
	// FromStepName is the step the task left to arrive here, empty when the move
	// had no recorded source or the source row is gone. The two names are
	// reported separately because a move reads as a change of place, and a step
	// name sitting beside the actor is read as a phrase instead.
	FromStepName string    `json:"from_step_name,omitempty"`
	StepName     string    `json:"step_name,omitempty"`
	At           time.Time `json:"at"`
	// Actor is what kind of thing moved the task, and Trigger is why, both as
	// the codes the ledger records. The client phrases them.
	Actor   string `json:"actor,omitempty"`
	Trigger string `json:"trigger,omitempty"`
	Stopped bool   `json:"stopped,omitempty"`
	// Repeat counts the movements of the same pair of steps that collapsed into
	// this one line, so a run that went back and forth reads as one entry with a
	// count rather than as the same move repeated.
	Repeat int `json:"repeat,omitempty"`
	// SentBack marks arriving at a step the task had already left earlier in
	// this same run, and Reopened marks arriving from a step that starts
	// nothing on entry, so a move out of finished or held work is not read as
	// ordinary progress. Reopened wins when both would apply.
	SentBack bool `json:"sent_back,omitempty"`
	Reopened bool `json:"reopened,omitempty"`
}

// OverviewFailure is what a failed session was followed by, read fresh on every
// overview pass. It carries codes and values rather than sentences: the client
// composes the line, so the same facts read in the viewer's language and stay
// honest about what is not known.
//
// The three follow-ups are independent — a task can have a later session, a
// routing fallback, and no terminal state at once — so each is reported on its
// own. HasNoAction is the server's own verdict that none of the three found
// anything, which is what the screen highlights rather than leaving an
// uneventful row to be read as healthy.
type OverviewFailure struct {
	// FailedAgoMinutes is how long the failure has stood as of this read, so
	// the number moves with the screen instead of only when a failure changes.
	FailedAgoMinutes int `json:"failed_ago_minutes"`
	// HasNoAction reports that no later session, no routing fallback, and no
	// terminal task state were found after the failure.
	HasNoAction bool `json:"has_no_action"`

	// NextSession is the first session of the same task started after the
	// failure. LaterSessions counts every session after it, so a task that
	// churned through several is not reported as a single retry.
	NextSession *OverviewFollowupSession `json:"next_session,omitempty"`
	// RouteReason is the routing reason the failed session's replacement
	// recorded, and RouteAttempts how many candidates dynamic routing tried
	// for that session.
	RouteReason   string `json:"route_reason,omitempty"`
	RouteAttempts int    `json:"route_attempts,omitempty"`
	// TaskState is the task's state as of this read. A task still open simply
	// reports its open state; the client distinguishes terminal from not.
	TaskState string `json:"task_state,omitempty"`
}

// OverviewFollowupSession is the session that ran after a failure, and what
// became of it. LaterSessions counts every session started after this one on the
// same task, so a task that churned through several is not reported as a single
// retry.
type OverviewFollowupSession struct {
	SessionID         string    `json:"session_id"`
	ModelName         string    `json:"model_name,omitempty"`
	State             string    `json:"state"`
	StartedAt         time.Time `json:"started_at"`
	StartedAgoMinutes int       `json:"started_ago_minutes"`
	LaterSessions     int       `json:"later_sessions,omitempty"`
}

// OverviewHumanItem is one thing waiting on a person. Repeats of the same
// question on the same task collapse into one item carrying Count, so a
// question an agent re-asks after every restart is one row.
type OverviewHumanItem struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	TaskID        string `json:"task_id,omitempty"`
	TaskTitle     string `json:"task_title,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	ApprovalType  string `json:"approval_type,omitempty"`
	// Count is how many occurrences this row stands for; always at least one.
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`

	// questionKey is the identity of what is being asked, used only while
	// assembling the list to merge repeats. It never reaches the wire.
	questionKey string
}

// OverviewTaskItem is one row of a task list.
type OverviewTaskItem struct {
	TaskID         string          `json:"task_id"`
	Title          string          `json:"title"`
	StepName       string          `json:"step_name"`
	State          string          `json:"state"`
	WorkspaceID    string          `json:"workspace_id"`
	WorkspaceName  string          `json:"workspace_name"`
	Status         string          `json:"status"`
	Reason         *OverviewReason `json:"reason,omitempty"`
	SessionID      string          `json:"session_id,omitempty"`
	SessionState   string          `json:"session_state,omitempty"`
	AgentProfileID string          `json:"agent_profile_id,omitempty"`
	ModelName      string          `json:"model_name,omitempty"`
	Failures24h    int             `json:"failures_24h"`
	LastOutputAt   *time.Time      `json:"last_output_at,omitempty"`
	StepEnteredAt  time.Time       `json:"step_entered_at"`
	// CompletedAt is the instant the task entered a completing step. Only a
	// completed row carries it, and it is what the window counts rather than
	// the task's last write.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// Archived reports that the task has left the board. A completed list is
	// mostly archived rows, so without this the reader cannot tell why a task
	// they just saw finish is not on any board.
	Archived       bool `json:"archived,omitempty"`
	QueuedMessages int  `json:"queued_messages"`
	// WaitingInputSessions is how many of this task's own sessions are waiting
	// on a person, so the expanded list answers that per row rather than leaving
	// the reader to add up the scope's one waiting figure.
	WaitingInputSessions int `json:"waiting_input_sessions"`
	// Failure is what followed this row's failed session, so a task that is in
	// trouble reports what became of the failure on its own row rather than only
	// in the events list. It is the same value the failed-session event carries.
	Failure *OverviewFailure `json:"failure,omitempty"`
}

// OverviewSessionItem is one row of the running-sessions list.
type OverviewSessionItem struct {
	SessionID      string          `json:"session_id"`
	TaskID         string          `json:"task_id"`
	TaskTitle      string          `json:"task_title"`
	WorkspaceID    string          `json:"workspace_id"`
	WorkspaceName  string          `json:"workspace_name"`
	AgentProfileID string          `json:"agent_profile_id,omitempty"`
	ModelName      string          `json:"model_name,omitempty"`
	SessionState   string          `json:"session_state"`
	Status         string          `json:"status"`
	Reason         *OverviewReason `json:"reason,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	LastOutputAt   *time.Time      `json:"last_output_at,omitempty"`
	// Failure reports what followed this session's failure. A row for a live
	// session has none: there is no failure to follow up on yet.
	Failure *OverviewFailure `json:"failure,omitempty"`
}

// OverviewQueueItem is one receiving session in the queued-messages list.
type OverviewQueueItem struct {
	SessionID     string          `json:"session_id"`
	TaskID        string          `json:"task_id"`
	TaskTitle     string          `json:"task_title"`
	WorkspaceID   string          `json:"workspace_id"`
	WorkspaceName string          `json:"workspace_name"`
	Status        string          `json:"status"`
	Reason        *OverviewReason `json:"reason,omitempty"`
	Count         int             `json:"count"`
	OldestAt      time.Time       `json:"oldest_at"`
	Sender        string          `json:"sender"`
	SessionState  string          `json:"session_state"`
	FirstLine     string          `json:"first_line,omitempty"`
}

// OverviewListResponse is the body of the two list routes. Exactly one of
// Tasks, Sessions, or Queue is populated; Total is the unclipped row count.
type OverviewListResponse struct {
	Kind     string                `json:"kind"`
	Filter   string                `json:"filter,omitempty"`
	Total    int                   `json:"total"`
	Tasks    []OverviewTaskItem    `json:"tasks,omitempty"`
	Sessions []OverviewSessionItem `json:"sessions,omitempty"`
	Queue    []OverviewQueueItem   `json:"queue,omitempty"`
}
