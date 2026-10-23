import type {
  NativeDashboard,
  NativeSharedCapacityWaiting,
  NativeTask,
} from "@/lib/types/control-native";
import type { SessionCapacityObservation } from "@/lib/types/system";

const GRADE_WAIT_REASONS = ["ceiling_grade_limit", "ceiling_both_limits"] as const;

/** Task states the board counts as work in progress. */
export const ACTIVE_TASK_STATES = ["IN_PROGRESS", "REVIEW", "SCHEDULING"] as const;
/** Task states that end a task; a failed session on one of these is history, not trouble. */
export const TERMINAL_TASK_STATES = new Set(["COMPLETED", "CANCELLED"]);

export type WorkspaceFigures = {
  inProgress: number;
  running: number;
  waitingInput: number;
  wipWaiting: number;
  queued: number;
  completed24h: number;
  moves24h: number;
  failedSessions: number;
};

/** The comparison figures one recorded dashboard yields. */
export function workspaceFigures(data: NativeDashboard): WorkspaceFigures {
  return {
    inProgress: ACTIVE_TASK_STATES.reduce((sum, state) => sum + (data.tasks[state] ?? 0), 0),
    running: data.sessions.RUNNING ?? 0,
    waitingInput: data.waiting_input_tasks,
    wipWaiting: data.wip_waiting_tasks,
    queued: data.queued_messages,
    completed24h: data.completed_tasks_24h,
    moves24h: data.transitions_24h,
    failedSessions: data.sessions.FAILED ?? 0,
  };
}

export function sumFigures(rows: WorkspaceFigures[]): WorkspaceFigures {
  const total: WorkspaceFigures = {
    inProgress: 0,
    running: 0,
    waitingInput: 0,
    wipWaiting: 0,
    queued: 0,
    completed24h: 0,
    moves24h: 0,
    failedSessions: 0,
  };
  for (const row of rows) {
    for (const key of Object.keys(total) as (keyof WorkspaceFigures)[]) total[key] += row[key];
  }
  return total;
}

/**
 * The installation-wide shared-capacity figure. Every workspace response
 * carries the same snapshot, so this picks the first one and never sums.
 */
export function sharedCapacityWaiting(
  rows: ReadonlyArray<NativeDashboard | undefined>,
): NativeDashboard["shared_capacity_waiting"] | undefined {
  for (const row of rows) {
    if (row?.shared_capacity_waiting) return row.shared_capacity_waiting;
  }
  return undefined;
}

/**
 * The board WIP figure is only complete when every workspace dashboard read
 * succeeded; a partial sum reads as unknown instead of a smaller number.
 */
export function wipWaitingFigure(
  complete: boolean,
  total: Pick<WorkspaceFigures, "wipWaiting">,
): number | null {
  return complete ? total.wipWaiting : null;
}

/**
 * Waiters refused by a grade limit, from the aggregate snapshot when present or
 * the observation's reason counts otherwise. Unknown is never read as zero.
 */
export function capacityGradeWaiting(
  observation: SessionCapacityObservation | undefined,
  known: boolean,
  sharedWaiting?: NativeSharedCapacityWaiting,
): number | null {
  if (sharedWaiting) return sharedWaiting.known && known ? sharedWaiting.grade : null;
  if (!known || observation?.waiting?.known !== true) return null;
  const counts = observation.waiting.counts;
  if (!counts) return null;
  const values = GRADE_WAIT_REASONS.map((reason) => counts[reason]);
  if (!values.every((value) => Number.isSafeInteger(value) && value >= 0)) return null;
  return values.reduce((sum, value) => sum + value, 0);
}

/**
 * Sessions waiting on the shared capacity ceiling, or null when the observation
 * does not report a complete set of counts. Unknown is never read as zero.
 */
export function capacityWaitingTotal(observation?: SessionCapacityObservation): number | null {
  if (!observation?.waiting?.known || !observation.waiting.counts) return null;
  const values = Object.values(observation.waiting.counts);
  if (!values.every((value) => Number.isSafeInteger(value) && value >= 0)) return null;
  return values.reduce((sum, value) => sum + value, 0);
}

export type WorkspaceStatus = "stalled" | "capacity" | "active" | "idle" | "pending" | "unknown";

/**
 * Recorded WIP without a running session is not proof of a stall. Holds,
 * workflow steps without execution actions and paused queues can all wait
 * legitimately. Installation-wide capacity counts cannot attribute a wait
 * to this workspace either; an incomplete observation remains unknown.
 */
export function workspaceStatus(
  figures: WorkspaceFigures,
  waiting: number | null,
  recordedTasks?: NativeDashboard["tasks"],
): WorkspaceStatus {
  if (figures.running > 0) return "active";
  const pending = recordedTasks
    ? Object.entries(recordedTasks).some(
        ([state, count]) => count > 0 && !TERMINAL_TASK_STATES.has(state.toUpperCase()),
      )
    : figures.inProgress > 0 || figures.queued > 0 || figures.waitingInput > 0;
  if (!pending) return "idle";
  return waiting === null ? "unknown" : "pending";
}

export type TaskSignal =
  | "repeat"
  | "stalled"
  | "failed"
  | "running"
  | "waiting"
  | "held"
  | "paused"
  | "queued"
  | "noSession"
  | "other";

/** Signals that need a person, in the order the attention lists rank them. */
export const ATTENTION_SIGNALS: TaskSignal[] = ["repeat", "stalled", "failed"];
const SIGNAL_RANK: TaskSignal[] = [
  ...ATTENTION_SIGNALS,
  "running",
  "waiting",
  "held",
  "paused",
  "queued",
  "other",
  "noSession",
];

export function isTerminalTask(task: Pick<NativeTask, "state" | "archived">): boolean {
  return task.archived || TERMINAL_TASK_STATES.has(task.state.toUpperCase());
}

export type TaskWaitReason =
  | "hold"
  | "approval"
  | "blocked"
  | "paused"
  | "queued"
  | "queueUnknown"
  | "waiting"
  | "failed"
  | "unknown";

/** Uses recorded fields only, never task/step names, Plan prose or approval guesses. */
export function taskWaitReason(task: NativeTask): TaskWaitReason | null {
  if (isTerminalTask(task) || task.running_sessions > 0 || task.session_state === "RUNNING")
    return null;
  if (task.session_state === "FAILED") return "failed";
  if (task.hold_mode || task.stage_type === "hold") return "hold";
  if (task.stage_type === "approval") return "approval";
  if (task.state.toUpperCase() === "BLOCKED") return "blocked";
  if (task.queued_messages > 0) {
    if ((task.queued_paused_messages ?? 0) > 0) return "paused";
    return task.queued_auto_run_messages === undefined ? "queueUnknown" : "queued";
  }
  if (task.session_state === "WAITING_FOR_INPUT") return "waiting";
  return "unknown";
}

/** One task's most urgent signal; `repeating` holds tasks flagged by the move-rate rule. */
const WAIT_SIGNALS: Partial<Record<TaskWaitReason, TaskSignal>> = {
  hold: "held",
  approval: "held",
  blocked: "held",
  paused: "paused",
  queued: "queued",
  queueUnknown: "queued",
};

export function taskSignal(task: NativeTask, repeating: ReadonlySet<string>): TaskSignal {
  if (repeating.has(task.task_id) && !isTerminalTask(task)) return "repeat";
  if (task.running_sessions > 0 || task.session_state === "RUNNING") return "running";
  if (task.session_state === "FAILED" && !isTerminalTask(task)) return "failed";
  if (!isTerminalTask(task)) {
    const reason = taskWaitReason(task);
    const signal = reason && WAIT_SIGNALS[reason];
    if (signal) return signal;
  }
  if (task.session_state === "WAITING_FOR_INPUT") return "waiting";
  if (!task.session_id) return "noSession";
  return "other";
}

export function signalRank(signal: TaskSignal): number {
  return SIGNAL_RANK.indexOf(signal);
}

/** Attention first, then running and waiting work; most recent change first within a signal. */
export function sortBySignal<T extends { signal: TaskSignal; task: NativeTask }>(rows: T[]): T[] {
  return [...rows].sort(
    (a, b) =>
      signalRank(a.signal) - signalRank(b.signal) ||
      Date.parse(b.task.updated_at) - Date.parse(a.task.updated_at),
  );
}
