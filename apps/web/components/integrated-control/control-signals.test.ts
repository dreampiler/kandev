import { describe, expect, it } from "vitest";
import type { NativeDashboard } from "@/lib/types/control-native";
import type { SessionCapacityObservation } from "@/lib/types/system";
import {
  capacityGradeWaiting,
  sharedCapacityWaiting,
  sumFigures,
  workspaceFigures,
} from "./control-signals";

function dashboard(overrides: Partial<NativeDashboard> = {}): NativeDashboard {
  return {
    workspace_id: "w",
    generated_at: "2026-10-19T03:00:00Z",
    since: "2026-10-18T03:00:00Z",
    basis: "recorded_task_session_queue_transition_state",
    inference_state: "unknown",
    tasks: {},
    sessions: {},
    waiting_input_tasks: 0,
    wip_waiting_tasks: 0,
    queued_messages: 0,
    transitions_24h: 0,
    completed_tasks_24h: 0,
    ...overrides,
  };
}

describe("control waiting figures", () => {
  it("sums board WIP waiters across workspaces", () => {
    const first = workspaceFigures(dashboard({ wip_waiting_tasks: 2 }));
    const second = workspaceFigures(dashboard({ wip_waiting_tasks: 1 }));
    expect(first.wipWaiting).toBe(2);
    expect(sumFigures([first, second]).wipWaiting).toBe(3);
  });

  it("reads the installation-wide shared figure once instead of summing it", () => {
    const first = dashboard({
      shared_capacity_waiting: { known: true, grade: 2, total: 3 },
    });
    const second = dashboard({
      shared_capacity_waiting: { known: true, grade: 2, total: 3 },
    });
    expect(sharedCapacityWaiting([first, second])).toEqual({ known: true, grade: 2, total: 3 });
    expect(sharedCapacityWaiting([undefined, first])).toEqual({ known: true, grade: 2, total: 3 });
    expect(sharedCapacityWaiting([undefined])).toBeUndefined();
  });

  it("counts grade-limit and both-limit waiters and keeps unknown separate", () => {
    const observation = {
      waiting: {
        known: true,
        counts: {
          ceiling_total_limit: 1,
          ceiling_grade_limit: 2,
          ceiling_both_limits: 1,
          ceiling_needs_configuration: 0,
          ceiling_unknown_population: 0,
        },
      },
      observed_at: "2026-10-19T03:00:00Z",
      enabled: true,
      known: true,
      in_use: 0,
      limit: 8,
      policy: "all",
    } as SessionCapacityObservation;
    expect(capacityGradeWaiting(observation, true)).toBe(3);
    expect(capacityGradeWaiting(observation, false)).toBeNull();
    expect(capacityGradeWaiting(observation, true, { known: true, grade: 2, total: 4 })).toBe(2);
    expect(
      capacityGradeWaiting(observation, true, { known: false, grade: 0, total: 0 }),
    ).toBeNull();
    expect(capacityGradeWaiting({ ...observation, waiting: { known: false } }, true)).toBeNull();
  });
});
