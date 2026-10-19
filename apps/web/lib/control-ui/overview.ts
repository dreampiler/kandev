import type {
  OverviewListResponse,
  OverviewSections,
  OverviewEvent,
  OverviewTaskFilter,
  OverviewWorkspaceMetrics,
  OverviewProjectionStatus,
} from "@/lib/types/control-overview";
import { parseStrictRfc3339Timestamp } from "@/lib/utils/strict-timestamp";

const METRIC_COUNTS = [
  "active_tasks",
  "running_sessions",
  "waiting_input_sessions",
  "queued_messages",
  "completed_24h",
  "open_tasks",
  "waiting_tasks",
  "blocked_tasks",
  "blocked_by_tasks",
] as const;

/** Partial wire projections cannot turn missing measurements into zero or NaN. */
export function hasWorkspaceMetrics(
  value: OverviewWorkspaceMetrics | undefined,
): value is OverviewWorkspaceMetrics {
  if (!value) return false;
  return (
    METRIC_COUNTS.every((key) => measuredCount(value[key])) &&
    Boolean(value.problems) &&
    [value.problems.error, value.problems.stalled, value.problems.delayed].every(measuredCount)
  );
}

function measuredCount(value: number) {
  return Number.isSafeInteger(value) && value >= 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function overviewTimestamp(data: unknown) {
  if (
    !isRecord(data) ||
    !Array.isArray(data.workspaces) ||
    data.scope !== "reachable" ||
    typeof data.generated_at !== "string"
  )
    return null;
  return parseStrictRfc3339Timestamp(data.generated_at);
}

/** Old fork responses have no metadata; new projections retain their explicit source state. */
export function overviewProjection(
  data: unknown,
  family: "history" | "workspace_metrics",
): OverviewProjectionStatus | undefined {
  if (!isRecord(data) || !isRecord(data.projection) || data.projection[family] === undefined)
    return undefined;
  const status = data.projection[family];
  if (
    isRecord(status) &&
    ["known", "partial", "unavailable"].includes(String(status.state)) &&
    Array.isArray(status.reasons) &&
    status.reasons.every((reason) => typeof reason === "string")
  )
    return status as OverviewProjectionStatus;
  return { state: "unavailable", reasons: ["invalid_projection_metadata"] };
}

/** Only the requested authorized workspace can contribute its own measured figures. */
export function overviewWorkspaceMetrics(data: unknown, workspaceId: string) {
  if (overviewProjection(data, "workspace_metrics")?.state === "unavailable") return undefined;
  if (overviewTimestamp(data) === null || !isRecord(data) || !Array.isArray(data.workspaces))
    return undefined;
  const workspace = data.workspaces.find(
    (item: unknown) => isRecord(item) && item.workspace_id === workspaceId,
  );
  return isRecord(workspace) &&
    hasWorkspaceMetrics(workspace.metrics as OverviewWorkspaceMetrics | undefined)
    ? (workspace.metrics as OverviewWorkspaceMetrics)
    : undefined;
}

const EVENT_KINDS = new Set([
  "server_started",
  "task_created",
  "task_completed",
  "session_failed",
  "automation_run",
  "model_blocked",
  "model_unblocked",
  "pr_merged",
  "automation_failed",
  "owner_decision",
  "step_move",
]);
const HISTORY_WINDOW_NS = BigInt("86400000000000");
const EVENT_TEXT_FIELDS = [
  "workspace_id",
  "automation_id",
  "task_id",
  "session_id",
  "title",
  "detail",
  "scope",
  "scope_value",
  "profile_id",
  "profile_name",
  "model_name",
  "reason",
  "from",
  "to",
  "version",
];

function isHistoryEvent(value: unknown, now: bigint): value is OverviewEvent {
  if (
    !isRecord(value) ||
    typeof value.kind !== "string" ||
    !EVENT_KINDS.has(value.kind) ||
    typeof value.at !== "string"
  )
    return false;
  const at = parseStrictRfc3339Timestamp(value.at);
  if (at === null || at > now || at < now - HISTORY_WINDOW_NS) return false;
  return EVENT_TEXT_FIELDS.every(
    (key) => value[key] === undefined || typeof value[key] === "string",
  );
}

/** The fixed history window is anchored to the server's read, including exact boundaries. */
export function overviewEvents(data: (OverviewSections & { workspaces?: unknown[] }) | undefined) {
  if (overviewProjection(data, "history")?.state === "unavailable") return undefined;
  const now = overviewTimestamp(data);
  if (now === null || !data) return undefined;
  if (data.last_24h === undefined) return [];
  if (!Array.isArray(data.last_24h) || !data.last_24h.every((event) => isHistoryEvent(event, now)))
    return undefined;
  return data.last_24h;
}

/** A generic Office task cursor response is not the aggregate task-list contract. */
export function overviewTaskRows(
  data: OverviewListResponse | undefined,
  workspaceId: string,
  filter?: OverviewTaskFilter,
) {
  if (data?.kind !== "tasks" || !Number.isSafeInteger(data.total) || data.total < 0)
    return undefined;
  if (filter !== undefined && data.filter !== filter) return undefined;
  if (data.total === 0 && !data.tasks) return [];
  if (!Array.isArray(data.tasks) || data.tasks.length > data.total) return undefined;
  return data.tasks.every(
    (task) =>
      isRecord(task) &&
      task.workspace_id === workspaceId &&
      typeof task.task_id === "string" &&
      task.task_id.length > 0 &&
      [task.title, task.state, task.step_name].every((value) => typeof value === "string"),
  )
    ? data.tasks
    : undefined;
}
