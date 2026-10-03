package dashboard

import "time"

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

// Overview event kinds for the last-24-hours section.
const (
	overviewEventServerStarted = "server_started"
	overviewEventTaskCompleted = "task_completed"
	overviewEventSessionFailed = "session_failed"
	overviewEventAutomationRun = "automation_run"
)

// OverviewReason is a machine-readable explanation of a status.
type OverviewReason struct {
	Code   string         `json:"code"`
	Values map[string]any `json:"values,omitempty"`
	Detail string         `json:"detail,omitempty"`
}

// OverviewSystem is the system card row.
type OverviewSystem struct {
	StartedAt             *time.Time `json:"started_at,omitempty"`
	ActiveTasks           int        `json:"active_tasks"`
	RunningSessions       int        `json:"running_sessions"`
	WaitingInputSessions  int        `json:"waiting_input_sessions"`
	SessionLimit          int        `json:"session_limit"`
	QueuedMessages        int        `json:"queued_messages"`
	UndeliverableMessages int        `json:"undeliverable_messages"`
	NeedsHuman            int        `json:"needs_human"`
	BlockedAccounts       int        `json:"blocked_accounts"`
	EarliestUnblockAt     *time.Time `json:"earliest_unblock_at,omitempty"`
	Problems              int        `json:"problems"`
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

// OverviewWorkspaceMetrics are the six project-card metrics plus problems.
type OverviewWorkspaceMetrics struct {
	Status               string                `json:"status"`
	ActiveTasks          int                   `json:"active_tasks"`
	RunningSessions      int                   `json:"running_sessions"`
	WaitingInputSessions int                   `json:"waiting_input_sessions"`
	LastOutputAt         *time.Time            `json:"last_output_at,omitempty"`
	LastOutputTaskID     string                `json:"last_output_task_id,omitempty"`
	QueuedMessages       int                   `json:"queued_messages"`
	Completed24h         int                   `json:"completed_24h"`
	OpenTasks            int                   `json:"open_tasks"`
	WaitingTasks         int                   `json:"waiting_tasks"`
	BlockedTasks         int                   `json:"blocked_tasks"`
	Problems             OverviewProblemCounts `json:"problems"`
	TopWarning           *OverviewWarning      `json:"top_warning,omitempty"`
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

// OverviewModel is one agent profile in the models section.
type OverviewModel struct {
	AgentProfileID string              `json:"agent_profile_id"`
	AgentID        string              `json:"agent_id"`
	AgentName      string              `json:"agent_name"`
	Name           string              `json:"name"`
	Sessions24h    int                 `json:"sessions_24h"`
	Running        int                 `json:"running"`
	Failed24h      int                 `json:"failed_24h"`
	Errors         []OverviewErrorKind `json:"errors"`
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
	Kind        string    `json:"kind"`
	At          time.Time `json:"at"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	TaskID      string    `json:"task_id,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Title       string    `json:"title,omitempty"`
	Detail      string    `json:"detail,omitempty"`
}

// OverviewHumanItem is one thing waiting on a person.
type OverviewHumanItem struct {
	Kind          string    `json:"kind"`
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	TaskID        string    `json:"task_id,omitempty"`
	TaskTitle     string    `json:"task_title,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	ApprovalType  string    `json:"approval_type,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
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
	QueuedMessages int             `json:"queued_messages"`
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
