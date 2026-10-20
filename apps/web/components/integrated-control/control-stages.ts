import type { NativeDashboard, NativeTask } from "@/lib/types/control-native";

/**
 * The stage a recorded task is in. The control projection classifies by task
 * state; a step's `stage_type` only fills in when the state names no stage.
 * "Completed" means the task completed but was not archived, so it still sits
 * on a step. Failed and cancelled tasks belong to no stage, so a stage total
 * never counts them.
 */
export const TASK_STAGES = ["startable", "running", "review", "held", "completed"] as const;

export type TaskStage = (typeof TASK_STAGES)[number];
export type TaskStageCounts = Record<TaskStage, number>;

const STAGE_BY_STATE: Record<string, TaskStage> = {
  CREATED: "startable",
  SCHEDULING: "startable",
  TODO: "startable",
  IN_PROGRESS: "running",
  REVIEW: "review",
  BLOCKED: "held",
  WAITING_FOR_INPUT: "held",
  COMPLETED: "completed",
};

function stageByType(stageType?: string): TaskStage | null {
  if (stageType === "review") return "review";
  if (stageType === "approval") return "held";
  return null;
}

export function taskStage(task: Pick<NativeTask, "state" | "stage_type">): TaskStage | null {
  return STAGE_BY_STATE[task.state?.toUpperCase() ?? ""] ?? stageByType(task.stage_type);
}

export function emptyStageCounts(): TaskStageCounts {
  return { startable: 0, running: 0, review: 0, held: 0, completed: 0 };
}

export function stageCounts(
  tasks: ReadonlyArray<Pick<NativeTask, "state" | "stage_type">>,
): TaskStageCounts {
  const counts = emptyStageCounts();
  for (const task of tasks) {
    const stage = taskStage(task);
    if (stage) counts[stage] += 1;
  }
  return counts;
}

/** The recorded dashboard counts every unarchived task by state. */
export function stageCountsFromStates(states: NativeDashboard["tasks"]): TaskStageCounts {
  const counts = emptyStageCounts();
  for (const [state, count] of Object.entries(states)) {
    if (!Number.isSafeInteger(count) || count < 0) continue;
    const stage = STAGE_BY_STATE[state.toUpperCase()];
    if (stage) counts[stage] += count;
  }
  return counts;
}

export function sumStageCounts(rows: ReadonlyArray<TaskStageCounts>): TaskStageCounts {
  return rows.reduce((total, row) => {
    for (const stage of TASK_STAGES) total[stage] += row[stage];
    return total;
  }, emptyStageCounts());
}

export function taskStageTotal(counts: TaskStageCounts): number {
  return TASK_STAGES.reduce((sum, stage) => sum + counts[stage], 0);
}
