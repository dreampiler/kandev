import type { TFunction } from "i18next";
import { formatCompactDuration, formatRelativeTime } from "@/lib/i18n/formats";
import type {
  OverviewQueueItem,
  OverviewQueueStatus,
  OverviewReason,
  OverviewStatus,
} from "@/lib/state/slices/office/overview-types";

// Catalog keys, not copy. The record keys are wire codes from the overview
// API; every sentence is phrased here so the server never sends prose.

const STATUS_LABEL_KEYS: Record<OverviewStatus, string> = {
  error: "common:error",
  stalled: "office:overviewStatusStalled",
  delayed: "office:overviewStatusDelayed",
  running: "common:sessionStateRunning",
  waiting: "threads:statusWaiting",
  blocked: "office:projectStatusOnHold",
};

const QUEUE_STATUS_LABEL_KEYS: Record<OverviewQueueStatus, string> = {
  undeliverable: "office:overviewQueueUndeliverable",
  delayed: "office:overviewStatusDelayed",
  waiting: "threads:statusWaiting",
};

const SESSION_STATE_LABEL_KEYS: Record<string, string> = {
  CREATED: "common:sessionStateCreated",
  STARTING: "common:sessionStateStarting",
  RUNNING: "common:sessionStateRunning",
  IDLE: "common:sessionStateIdle",
  WAITING_FOR_INPUT: "common:sessionStateWaitingForInput",
  COMPLETED: "common:sessionStateCompleted",
  FAILED: "common:sessionStateFailed",
  CANCELLED: "common:sessionStateCancelled",
};

const SENDER_LABEL_KEYS: Record<OverviewQueueItem["sender"], string> = {
  user: "canvases:sourceActorUser",
  agent: "common:agent",
  workflow: "common:workflow",
  system: "common:system",
};

/** Reason codes phrased by a fixed catalog entry with no values. */
const PLAIN_REASON_KEYS: Record<string, string> = {
  session_failed: "office:overviewReasonSessionFailed",
  session_error: "office:overviewReasonSessionError",
  task_failed: "office:overviewReasonTaskFailed",
  starting_with_output: "office:overviewReasonStartingWithOutput",
  session_ended: "office:overviewReasonSessionEnded",
  working: "common:sessionStateRunning",
  waiting_input: "common:sessionStateWaitingForInput",
  not_started: "common:taskStateNotStarted",
  idle: "common:sessionStateIdle",
  on_hold: "office:projectStatusOnHold",
};

/** Reason codes phrased around an elapsed time in minutes. */
const DURATION_REASON_KEYS: Record<string, string> = {
  no_output: "office:overviewReasonNoOutput",
  starting_too_long: "office:overviewReasonStartingTooLong",
  not_advancing: "office:overviewReasonNotAdvancing",
  queue_not_delivered: "office:overviewReasonQueueNotDelivered",
  turn_not_finishing: "office:overviewReasonTurnNotFinishing",
};

export const PROBLEM_STATUSES: ReadonlySet<OverviewStatus> = new Set([
  "error",
  "stalled",
  "delayed",
]);

export function statusLabel(t: TFunction, status: OverviewStatus | undefined): string {
  return status ? t(STATUS_LABEL_KEYS[status]) : t("common:unknown");
}

export function queueStatusLabel(t: TFunction, status: OverviewQueueStatus): string {
  return t(QUEUE_STATUS_LABEL_KEYS[status]);
}

export function sessionStateLabel(t: TFunction, state: string | undefined): string {
  if (!state) return "";
  const key = SESSION_STATE_LABEL_KEYS[state];
  return key ? t(key) : state;
}

export function senderLabel(t: TFunction, sender: OverviewQueueItem["sender"]): string {
  return t(SENDER_LABEL_KEYS[sender] ?? "common:unknown");
}

/** A minute count as a compact locale duration ("31m"/"31분", "2h", "3d"). */
export function durationFromMinutes(minutes: number): string {
  if (minutes < 60) return formatCompactDuration(minutes, "minute");
  if (minutes < 24 * 60) return formatCompactDuration(minutes / 60, "hour");
  return formatCompactDuration(minutes / (24 * 60), "day");
}

/** Elapsed time since an ISO timestamp as a compact duration. */
export function durationSince(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "";
  const at = new Date(iso).getTime();
  if (Number.isNaN(at)) return "";
  return durationFromMinutes(Math.max(0, Math.floor((now - at) / 60_000)));
}

/** "12 minutes ago" in the active locale, or "" when absent. */
export function relativeTime(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "";
  return formatRelativeTime(iso, now);
}

function numberValue(reason: OverviewReason, name: string): number {
  const value = reason.values?.[name];
  return typeof value === "number" ? value : Number(value ?? 0);
}

/** Phrases a reason code and its values in the viewer's language. */
export function reasonText(t: TFunction, reason: OverviewReason | undefined): string {
  if (!reason) return "";
  const plain = PLAIN_REASON_KEYS[reason.code];
  if (plain) return t(plain);
  const timed = DURATION_REASON_KEYS[reason.code];
  if (timed) return t(timed, { duration: durationFromMinutes(numberValue(reason, "minutes")) });
  switch (reason.code) {
    case "recent_failures":
      return t("office:overviewReasonRecentFailures", { count: numberValue(reason, "count") });
    case "waiting_prereq":
      return t("office:overviewReasonWaitingPrereq", { count: numberValue(reason, "count") });
    case "step_dwell": {
      const duration = durationFromMinutes(numberValue(reason, "minutes"));
      const step = String(reason.values?.step ?? "");
      return step
        ? t("office:overviewReasonStepDwell", { duration, step })
        : t("office:overviewReasonStepDwellUnnamed", { duration });
    }
  }
  return reason.code;
}

/** Status color classes shared by badges and status dots. */
export function statusTone(status: OverviewStatus | OverviewQueueStatus | undefined): string {
  switch (status) {
    case "error":
    case "undeliverable":
      return "bg-destructive/10 text-destructive border-destructive/30";
    case "stalled":
      return "bg-orange-500/10 text-orange-600 border-orange-500/30 dark:text-orange-400";
    case "delayed":
      return "bg-amber-500/10 text-amber-700 border-amber-500/30 dark:text-amber-400";
    case "running":
      return "bg-emerald-500/10 text-emerald-700 border-emerald-500/30 dark:text-emerald-400";
    case "blocked":
      return "bg-violet-500/10 text-violet-700 border-violet-500/30 dark:text-violet-300";
    default:
      return "bg-muted text-muted-foreground border-border";
  }
}

/** Short display form of an id for table rows. */
export function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}
