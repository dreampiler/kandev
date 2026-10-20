import { describe, expect, it } from "vitest";
import {
  emptyStageCounts,
  stageCounts,
  stageCountsFromStates,
  sumStageCounts,
  TASK_STAGES,
  taskStage,
  taskStageTotal,
} from "./control-stages";

const task = (state: string, stage_type = "custom") => ({ state, stage_type });

describe("task stages", () => {
  it.each([
    ["CREATED", "startable"],
    ["TODO", "startable"],
    ["SCHEDULING", "startable"],
    ["IN_PROGRESS", "running"],
    ["REVIEW", "review"],
    ["BLOCKED", "held"],
    ["WAITING_FOR_INPUT", "held"],
    ["COMPLETED", "completed"],
  ])("maps %s to %s from the recorded state", (state, stage) => {
    expect(taskStage(task(state))).toBe(stage);
  });

  it("falls back to the step's stage type only when the state names no stage", () => {
    expect(taskStage(task("", "review"))).toBe("review");
    expect(taskStage(task("", "approval"))).toBe("held");
    expect(taskStage(task("UNKNOWN", "work"))).toBeNull();
  });

  it("never counts failed or cancelled work in a stage", () => {
    expect(taskStage(task("FAILED"))).toBeNull();
    expect(taskStage(task("CANCELLED"))).toBeNull();
    expect(taskStageTotal(stageCounts([task("FAILED"), task("CANCELLED")]))).toBe(0);
  });

  it("counts each stage once and totals only the five stages", () => {
    const counts = stageCounts([
      task("TODO"),
      task("IN_PROGRESS"),
      task("IN_PROGRESS"),
      task("REVIEW"),
      task("BLOCKED"),
      task("COMPLETED"),
      task("FAILED"),
    ]);
    expect(counts).toEqual({
      startable: 1,
      running: 2,
      review: 1,
      held: 1,
      completed: 1,
    });
    expect(taskStageTotal(counts)).toBe(6);
  });

  it("reads the dashboard's unarchived state counts without inventing values", () => {
    const counts = stageCountsFromStates({
      TODO: 3,
      IN_PROGRESS: 2,
      REVIEW: 1,
      WAITING_FOR_INPUT: 4,
      COMPLETED: 5,
      FAILED: 9,
      CANCELLED: 7,
    });
    expect(counts).toEqual({ startable: 3, running: 2, review: 1, held: 4, completed: 5 });
    expect(stageCountsFromStates({ TODO: -1, IN_PROGRESS: 1.5, COMPLETED: Number.NaN })).toEqual(
      emptyStageCounts(),
    );
  });

  it("sums workspaces stage by stage", () => {
    const total = sumStageCounts([
      { startable: 1, running: 2, review: 0, held: 0, completed: 1 },
      { startable: 0, running: 1, review: 3, held: 2, completed: 0 },
    ]);
    expect(total).toEqual({ startable: 1, running: 3, review: 3, held: 2, completed: 1 });
    expect(TASK_STAGES.reduce((sum, stage) => sum + total[stage], 0)).toBe(taskStageTotal(total));
  });
});
