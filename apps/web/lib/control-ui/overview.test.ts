import { describe, expect, it } from "vitest";
import {
  hasWorkspaceMetrics,
  overviewEvents,
  overviewTaskRows,
  overviewWorkspaceMetrics,
  overviewProjection,
} from "./overview";
import type { OverviewListResponse } from "@/lib/types/control-overview";
import type { OverviewWorkspaceMetrics } from "@/lib/types/control-overview";

const metrics: OverviewWorkspaceMetrics = {
  status: "waiting",
  active_tasks: 0,
  running_sessions: 0,
  waiting_input_sessions: 0,
  queued_messages: 0,
  completed_24h: 0,
  open_tasks: 0,
  waiting_tasks: 0,
  blocked_tasks: 0,
  blocked_by_tasks: 0,
  problems: { error: 0, stalled: 0, delayed: 0 },
};
describe("workspace measured projection", () => {
  it("accepts observed zero while rejecting absent or partial metrics", () => {
    expect(hasWorkspaceMetrics(metrics)).toBe(true);
    expect(hasWorkspaceMetrics(undefined)).toBe(false);
    expect(
      hasWorkspaceMetrics({
        ...metrics,
        open_tasks: undefined,
      } as unknown as OverviewWorkspaceMetrics),
    ).toBe(false);
    expect(hasWorkspaceMetrics({ ...metrics, running_sessions: -1 })).toBe(false);
  });
  it("respects the fork's omitted empty list without accepting an unmeasured projection", () => {
    expect(
      overviewEvents({ scope: "reachable", workspaces: [], generated_at: "2026-10-18T14:00:00Z" }),
    ).toEqual([]);
    expect(overviewEvents({ scope: "reachable", workspaces: [] })).toBeUndefined();
    expect(overviewEvents(undefined)).toBeUndefined();
  });
  it("rejects the observed generic 200 cursor list and accepts a measured empty overview", () => {
    expect(
      overviewTaskRows(
        { next_cursor: "", next_id: "", tasks: [] } as unknown as OverviewListResponse,
        "workspace",
      ),
    ).toBeUndefined();
    expect(overviewTaskRows({ kind: "tasks", total: 0 }, "workspace")).toEqual([]);
    expect(overviewTaskRows({ kind: "tasks", total: 1 }, "workspace")).toBeUndefined();
  });
});

describe("history and narrowed projection boundaries", () => {
  it("keeps the inclusive 24-hour boundary at nanosecond precision", () => {
    const data = {
      scope: "reachable" as const,
      workspaces: [],
      generated_at: "2026-10-19T00:00:00.123456789Z",
    };
    const event = {
      kind: "task_created" as const,
      at: "2026-10-18T00:00:00.123456789Z",
      title: "Boundary",
    };
    expect(overviewEvents({ ...data, last_24h: [event] })).toEqual([event]);
    expect(
      overviewEvents({ ...data, last_24h: [{ ...event, at: "2026-10-18T00:00:00.123456788Z" }] }),
    ).toBeUndefined();
    expect(
      overviewEvents({ ...data, last_24h: [{ ...event, at: "2026-10-19T00:00:00.123456790Z" }] }),
    ).toBeUndefined();
  });

  it.each([null, {}, "wrong-list", [null], [{ kind: "task_created", at: "2026-02-30T12:00:00Z" }]])(
    "rejects malformed history without throwing: %j",
    (events) => {
      const data = {
        scope: "reachable",
        workspaces: [],
        generated_at: "2026-10-19T00:00:00Z",
        last_24h: events,
      };
      expect(overviewEvents(data as never)).toBeUndefined();
    },
  );

  it("rejects a different filter, foreign rows and inconsistent task totals", () => {
    const row = {
      task_id: "task",
      workspace_id: "workspace",
      title: "Task",
      state: "TODO",
      step_name: "Todo",
    };
    const data = { kind: "tasks" as const, filter: "all" as const, total: 1, tasks: [row] };
    expect(overviewTaskRows(data as never, "workspace", "all")).toEqual([row]);
    expect(overviewTaskRows(data as never, "workspace", "completed")).toBeUndefined();
    expect(overviewTaskRows(data as never, "foreign", "all")).toBeUndefined();
    expect(overviewTaskRows({ ...data, total: 0 } as never, "workspace", "all")).toBeUndefined();
    expect(overviewTaskRows({ ...data, tasks: {} } as never, "workspace", "all")).toBeUndefined();
  });

  it("handles malformed workspace collections without crashing or inventing metrics", () => {
    const header = { scope: "reachable", generated_at: "2026-10-19T00:00:00Z" };
    expect(overviewWorkspaceMetrics({ ...header, workspaces: {} }, "workspace")).toBeUndefined();
    expect(
      overviewWorkspaceMetrics({ ...header, workspaces: [null] }, "workspace"),
    ).toBeUndefined();
    expect(
      overviewWorkspaceMetrics(
        { ...header, workspaces: [{ workspace_id: "workspace", metrics }] },
        "workspace",
      ),
    ).toEqual(metrics);
    expect(
      overviewWorkspaceMetrics(
        { ...header, workspaces: [{ workspace_id: "foreign", metrics }] },
        "workspace",
      ),
    ).toBeUndefined();
  });

  it("keeps partial supported rows, but never presents an unavailable source as measured empty", () => {
    const header = {
      scope: "reachable" as const,
      generated_at: "2026-10-19T00:00:00Z",
      workspaces: [{ workspace_id: "workspace", metrics }],
      last_24h: [],
    };
    const unavailable = { state: "unavailable" as const, reasons: ["source_read_failed"] };
    expect(overviewEvents({ ...header, projection: { history: unavailable } })).toBeUndefined();
    expect(
      overviewWorkspaceMetrics(
        { ...header, projection: { workspace_metrics: unavailable } },
        "workspace",
      ),
    ).toBeUndefined();
    const partial = { state: "partial" as const, reasons: ["server_start_history_not_supported"] };
    expect(overviewEvents({ ...header, projection: { history: partial } })).toEqual([]);
    expect(overviewProjection({ ...header, projection: { history: partial } }, "history")).toEqual(
      partial,
    );
    expect(
      overviewProjection({ projection: { history: { state: "broken" } } }, "history")?.state,
    ).toBe("unavailable");
  });
});
