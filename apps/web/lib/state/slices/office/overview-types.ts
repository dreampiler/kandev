/**
 * Wire shapes for the overview sections of the multi-workspace aggregate
 * (GET /api/v1/office/workspaces/aggregate) and its list routes. Mirrors the
 * snake_case JSON the backend serialises. Reasons are a code plus values so
 * the page phrases them in the viewer's language; `detail` is an agent's own
 * error text and is shown verbatim.
 */

export type OverviewScope = "office" | "reachable";

export type OverviewStatus = "error" | "stalled" | "delayed" | "running" | "waiting" | "blocked";

export type OverviewQueueStatus = "undeliverable" | "delayed" | "waiting";

export type OverviewReason = {
  code: string;
  values?: Record<string, string | number>;
  detail?: string;
};

export type OverviewSystem = {
  started_at?: string;
  active_tasks: number;
  running_sessions: number;
  waiting_input_sessions: number;
  session_limit: number;
  queued_messages: number;
  undeliverable_messages: number;
  needs_human: number;
  blocked_accounts: number;
  earliest_unblock_at?: string;
  problems: number;
};

export type OverviewWorkspaceMetrics = {
  status: OverviewStatus;
  active_tasks: number;
  running_sessions: number;
  waiting_input_sessions: number;
  last_output_at?: string;
  last_output_task_id?: string;
  queued_messages: number;
  completed_24h: number;
  open_tasks: number;
  waiting_tasks: number;
  blocked_tasks: number;
  problems: { error: number; stalled: number; delayed: number };
  top_warning?: {
    task_id: string;
    task_title: string;
    status: OverviewStatus;
    reason?: OverviewReason;
  };
};

export type OverviewParentTask = {
  task_id: string;
  title: string;
  status: OverviewStatus;
  children: number;
  open_children: number;
};

export type OverviewModel = {
  agent_profile_id: string;
  agent_id: string;
  agent_name: string;
  name: string;
  sessions_24h: number;
  running: number;
  failed_24h: number;
  errors: { kind: string; count: number }[] | null;
};

export type OverviewBlockedAccount = {
  workspace_id: string;
  provider_id: string;
  scope: string;
  scope_value: string;
  state: string;
  error_code?: string;
  retry_at?: string;
};

export type OverviewEventKind =
  | "server_started"
  | "task_completed"
  | "session_failed"
  | "automation_run";

export type OverviewEvent = {
  kind: OverviewEventKind;
  at: string;
  workspace_id?: string;
  automation_id?: string;
  task_id?: string;
  session_id?: string;
  title?: string;
  detail?: string;
};

export type OverviewHumanItem = {
  kind: "question" | "approval";
  id: string;
  workspace_id: string;
  workspace_name: string;
  task_id?: string;
  task_title?: string;
  session_id?: string;
  approval_type?: string;
  created_at: string;
};

export type OverviewTaskItem = {
  task_id: string;
  title: string;
  step_name: string;
  state: string;
  workspace_id: string;
  workspace_name: string;
  status?: OverviewStatus;
  reason?: OverviewReason;
  session_id?: string;
  session_state?: string;
  agent_profile_id?: string;
  model_name?: string;
  failures_24h: number;
  last_output_at?: string;
  step_entered_at: string;
  queued_messages: number;
};

export type OverviewSessionItem = {
  session_id: string;
  task_id: string;
  task_title: string;
  workspace_id: string;
  workspace_name: string;
  agent_profile_id?: string;
  model_name?: string;
  session_state: string;
  status: OverviewStatus;
  reason?: OverviewReason;
  started_at: string;
  last_output_at?: string;
};

export type OverviewQueueItem = {
  session_id: string;
  task_id: string;
  task_title: string;
  workspace_id: string;
  workspace_name: string;
  status: OverviewQueueStatus;
  reason?: OverviewReason;
  count: number;
  oldest_at: string;
  sender: "user" | "agent" | "workflow" | "system";
  session_state: string;
  first_line?: string;
};

export type OverviewTaskFilter =
  | "problems"
  | "active"
  | "sessions"
  | "queued"
  | "completed"
  | "hold"
  | "all";

export type OverviewRunningKind = "tasks" | "sessions" | "queue";

export type OverviewListResponse = {
  kind: OverviewRunningKind;
  filter?: OverviewTaskFilter;
  total: number;
  tasks?: OverviewTaskItem[];
  sessions?: OverviewSessionItem[];
  queue?: OverviewQueueItem[];
};

/** Overview sections carried on the aggregate response next to its counts. */
export type OverviewSections = {
  scope: OverviewScope;
  generated_at?: string;
  compute_ms?: number;
  system?: OverviewSystem;
  models?: OverviewModel[];
  blocked_accounts?: OverviewBlockedAccount[];
  last_24h?: OverviewEvent[];
  needs_human?: OverviewHumanItem[];
};
