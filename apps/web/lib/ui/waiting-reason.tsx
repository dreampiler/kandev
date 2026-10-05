import type { ComponentType } from "react";
import { IconClockHour4, IconHourglass, IconMessageQuestion } from "@tabler/icons-react";
import { useTranslation } from "react-i18next";
import { Tooltip, TooltipContent, TooltipTrigger } from "@kandev/ui/tooltip";
import { cn } from "@/lib/utils";
import type { Task, TaskSessionState } from "@/lib/types/http";
import type { TaskStatusSummaryLaunchQueue } from "@/lib/types/task-status-summary";

/**
 * Why a task is not doing work, when the reason is an admission or answer wait
 * rather than a workflow state. The workflow state alone cannot express these:
 * a task waiting for a step slot, a task waiting for a session slot and a task
 * whose session is waiting for an answer all land in the same REVIEW bucket,
 * so they used to render the one green "turn finished" icon.
 */
export type TaskWaitReason = "wip_queue" | "session_ceiling" | "awaiting_answer";

/** The admission facts every task-shaped surface already carries. */
export type WaitReasonTaskLike = {
  primarySessionState?: string | null;
  wipAdmitted?: boolean | null;
  queuedForStepId?: string | null;
  launchQueue?: TaskStatusSummaryLaunchQueue | null;
};

const WAITING_FOR_INPUT: TaskSessionState = "WAITING_FOR_INPUT";

/**
 * The one place that decides which wait a task is in. Precedence is fixed:
 * an answered-wait session outranks an admission wait (the session already
 * holds capacity, so the slot is not what it is waiting for), and an admission
 * wait outranks a WIP overflow (a session-capacity deferral is the harder
 * wait — it survives across turns, while a WIP slot frees as soon as a
 * neighbour finishes).
 *
 * Pending clarification and permission are deliberately NOT inputs: those own
 * their own icons upstream, so a task that is prompting for input keeps the
 * prompt affordance instead of being re-labelled as a plain answer wait.
 */
export function resolveWaitReason(task: WaitReasonTaskLike): TaskWaitReason | null {
  if (task.primarySessionState === WAITING_FOR_INPUT) return "awaiting_answer";
  if (task.launchQueue) return "session_ceiling";
  if (task.wipAdmitted !== true && task.queuedForStepId) return "wip_queue";
  return null;
}

/** Snake-case API task projection -> wait reason. */
export function resolveApiTaskWaitReason(
  task: Pick<
    Task,
    "primary_session_state" | "wip_admitted" | "queued_for_step_id" | "status_summary"
  >,
): TaskWaitReason | null {
  return resolveWaitReason({
    primarySessionState: task.primary_session_state,
    wipAdmitted: task.wip_admitted,
    queuedForStepId: task.queued_for_step_id,
    launchQueue: task.status_summary?.launch_queue,
  });
}

const WAIT_REASON_ICON: Record<TaskWaitReason, ComponentType<{ className?: string }>> = {
  wip_queue: IconHourglass,
  session_ceiling: IconClockHour4,
  awaiting_answer: IconMessageQuestion,
};

// Colors come from the shared status tone vocabulary (the same tokens the
// overview legend and its status dots use), so a wait reads as the same kind of
// state wherever it is shown, and so a new tone never gets spelled as a raw
// palette color at a call site.
const WAIT_REASON_CLASS: Record<TaskWaitReason, string> = {
  wip_queue: "text-status-info focus-visible:ring-status-info",
  session_ceiling: "text-status-delayed focus-visible:ring-status-delayed",
  awaiting_answer: "text-status-stalled focus-visible:ring-status-stalled",
};

const WAIT_REASON_LABEL_KEY: Record<TaskWaitReason, string> = {
  wip_queue: "task:waitingReasonWipQueue",
  session_ceiling: "task:waitingReasonSessionCapacity",
  awaiting_answer: "task:waitingReasonAwaitingAnswer",
};

const WAIT_REASON_HELP_KEY: Record<TaskWaitReason, string> = {
  wip_queue: "task:waitingReasonWipQueueHelp",
  session_ceiling: "task:waitingReasonSessionCapacityHelp",
  awaiting_answer: "task:waitingReasonAwaitingAnswerHelp",
};

export function waitReasonLabelKey(reason: TaskWaitReason): string {
  return WAIT_REASON_LABEL_KEY[reason];
}

export function waitReasonGlyph(reason: TaskWaitReason): ComponentType<{ className?: string }> {
  return WAIT_REASON_ICON[reason];
}

export function waitReasonClass(reason: TaskWaitReason): string {
  return WAIT_REASON_CLASS[reason];
}

type WaitReasonTooltipCopy = { label: string; help: string };

function useWaitReasonCopy(
  reason: TaskWaitReason,
  launchQueue: TaskStatusSummaryLaunchQueue | null | undefined,
): WaitReasonTooltipCopy {
  const { t } = useTranslation();
  const label = t(WAIT_REASON_LABEL_KEY[reason]);
  const capacity = launchQueue?.capacity;
  const help =
    reason === "session_ceiling" && capacity
      ? t("task:waitingReasonSessionCapacityOccupancy", {
          inUse: capacity.in_use,
          limit: capacity.limit,
        })
      : t(WAIT_REASON_HELP_KEY[reason]);
  return { label, help };
}

/**
 * Shared affordance for a task that is waiting rather than working: a distinct
 * icon per wait reason, carrying the accessible label and the one-line reason,
 * so every surface that renders it presents the same thing (the
 * InterruptedTaskIcon / BackgroundWorkTaskIcon precedent).
 */
export function WaitingReasonTaskIcon({
  reason,
  launchQueue,
  className,
}: {
  reason: TaskWaitReason;
  launchQueue?: TaskStatusSummaryLaunchQueue | null;
  className?: string;
}) {
  const { label, help } = useWaitReasonCopy(reason, launchQueue);
  const Icon = WAIT_REASON_ICON[reason];
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          aria-label={label}
          tabIndex={0}
          className={cn(
            "mt-[1px] flex shrink-0 rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-1",
            WAIT_REASON_CLASS[reason],
            className,
          )}
        >
          <Icon
            aria-hidden="true"
            data-testid={`task-state-waiting-${reason}`}
            className="h-3.5 w-3.5 shrink-0"
          />
        </span>
      </TooltipTrigger>
      <TooltipContent side="right">
        <span className="flex flex-col">
          <span className="font-medium">{label}</span>
          <span className="text-muted-foreground">{help}</span>
        </span>
      </TooltipContent>
    </Tooltip>
  );
}
