export type NativeCoverage = {
  state: "known" | "unavailable";
  reason?: string;
  total: number | null;
  returned: number;
  truncated: boolean;
};
export type NativeEvent = {
  id: string;
  kind: string;
  task_id?: string;
  session_id?: string;
  title?: string;
  state?: string;
  occurred_at: string;
};
export type NativeHistory = {
  workspace_id: string;
  generated_at: string;
  since: string;
  coverage: string;
  truncated: boolean;
  sources: Record<string, NativeCoverage>;
  events: NativeEvent[];
};
export type NativeDashboard = {
  workspace_id: string;
  generated_at: string;
  since: string;
  basis: "recorded_task_session_queue_transition_state";
  inference_state: "unknown";
  tasks: Record<string, number>;
  sessions: Record<string, number>;
  queued_messages: number;
  transitions_24h: number;
  completed_tasks_24h: number;
};
export type NativeTask = {
  task_id: string;
  workspace_id: string;
  title: string;
  state: string;
  step_id: string;
  step_name: string;
  stage_type: string;
  session_id: string;
  session_state: string;
  archived: boolean;
  running_sessions: number;
  queued_messages: number;
  updated_at: string;
};
export type NativeArchive = "active" | "archived" | "all";
export type NativeTaskList = {
  kind: "tasks";
  filter: string;
  archive: NativeArchive;
  total: number;
  truncated: boolean;
  generated_at: string;
  tasks: NativeTask[];
};
export type NativeModel = {
  model: string;
  provider: string;
  measurement_source: string;
  usage_completeness: string;
  cost_source: string;
  estimated: boolean;
  event_count: number;
  tokens_in: number;
  tokens_out: number | null;
  tokens_cached_read: number | null;
  tokens_cached_write: number | null;
  tokens_total: number;
  cost_subcents: number;
};
export type NativeLedger = {
  basis: "task_usage_events";
  workspace_id: string;
  since: string;
  generated_at: string;
  account_attribution: "unavailable";
  coverage: string;
  truncated: boolean;
  models: NativeModel[];
};
