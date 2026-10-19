import type { NativeDashboard, NativeTask } from "@/lib/types/control-native";
import type { SessionCapacityObservation } from "@/lib/types/system";

/** Task states the board counts as work in progress. */
export const ACTIVE_TASK_STATES = ["IN_PROGRESS", "REVIEW", "SCHEDULING"] as const;
/** Task states that end a task; a failed session on one of these is history, not trouble. */
export const TERMINAL_TASK_STATES = new Set(["COMPLETED", "CANCELLED"]);

export type WorkspaceFigures = {
  inProgress: number;
  running: number;
  waitingInput: number;
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
    waitingInput: data.sessions.WAITING_FOR_INPUT ?? 0,
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
 * Sessions waiting on the shared capacity ceiling, or null when the observation
 * does not report a complete set of counts. Unknown is never read as zero.
 */
export function capacityWaitingTotal(observation?: SessionCapacityObservation): number | null {
  if (!observation?.waiting?.known || !observation.waiting.counts) return null;
  const values = Object.values(observation.waiting.counts);
  if (!values.every((value) => Number.isSafeInteger(value) && value >= 0)) return null;
  return values.reduce((sum, value) => sum + value, 0);
}

export type WorkspaceStatus = "stalled" | "capacity" | "active" | "idle";

/**
 * Stalled: work is in progress, nothing runs, and nothing waits on the
 * capacity ceiling, so the ceiling is not the reason. When the ceiling's
 * waiting counts are unknown the stall cannot be told apart from a capacity
 * wait, so the workspace reads as active rather than as a claimed stall.
 */
export function workspaceStatus(
  figures: WorkspaceFigures,
  waiting: number | null,
): WorkspaceStatus {
  if (figures.inProgress === 0 && figures.running === 0) return "idle";
  if (figures.running > 0) return "active";
  if (waiting === 0) return "stalled";
  if (waiting !== null && waiting > 0) return "capacity";
  return "active";
}

export type TaskSignal =
  | "repeat"
  | "stalled"
  | "failed"
  | "running"
  | "waiting"
  | "noSession"
  | "other";

/** Signals that need a person, in the order the attention lists rank them. */
export const ATTENTION_SIGNALS: TaskSignal[] = ["repeat", "stalled", "failed"];
const SIGNAL_RANK: TaskSignal[] = [
  ...ATTENTION_SIGNALS,
  "running",
  "waiting",
  "other",
  "noSession",
];

export function isTerminalTask(task: Pick<NativeTask, "state" | "archived">): boolean {
  return task.archived || TERMINAL_TASK_STATES.has(task.state.toUpperCase());
}

/** One task's most urgent signal; `repeating` holds tasks flagged by the move-rate rule. */
export function taskSignal(task: NativeTask, repeating: ReadonlySet<string>): TaskSignal {
  if (repeating.has(task.task_id) && !isTerminalTask(task)) return "repeat";
  if (task.queued_messages > 0 && task.running_sessions === 0) return "stalled";
  if (task.session_state === "FAILED" && !isTerminalTask(task)) return "failed";
  if (task.running_sessions > 0 || task.session_state === "RUNNING") return "running";
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
