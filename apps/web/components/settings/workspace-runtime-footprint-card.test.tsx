import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const WORKSPACE_ID = "workspace-a";

afterEach(cleanup);

import type { WorkspaceRuntimeFootprint } from "@/lib/api/domains/runtime-footprint-api";

import { WorkspaceRuntimeFootprintCard } from "./workspace-runtime-footprint-card";

const OBSERVED_AT = "2026-10-03T00:00:00Z";

function footprint(overrides: Partial<WorkspaceRuntimeFootprint> = {}): WorkspaceRuntimeFootprint {
  return {
    liveRuntimes: 3,
    workspaceRuntimes: 2,
    processes: 6,
    committedBytes: 0,
    residentBytes: 0,
    unreadableRuntimes: 0,
    complete: true,
    observedAt: OBSERVED_AT,
    runtimes: [],
    ...overrides,
  };
}

describe("WorkspaceRuntimeFootprintCard", () => {
  it("shows the per-session rows and totals for a measured observation", async () => {
    const load = vi.fn().mockResolvedValue(
      footprint({
        processes: 6,
        committedBytes: 1024 * 1024 * 400,
        residentBytes: 1024 * 1024 * 300,
        runtimes: [
          {
            sessionId: "session-1",
            taskId: "task-1",
            status: "running",
            processes: 4,
            committedBytes: 1024 * 1024 * 300,
            residentBytes: 1024 * 1024 * 200,
            unreadableProcesses: 0,
            lastActivityAt: OBSERVED_AT,
          },
          {
            sessionId: "session-2",
            taskId: "task-2",
            status: "ready",
            processes: 2,
            committedBytes: 1024 * 1024 * 100,
            residentBytes: 1024 * 1024 * 100,
            unreadableProcesses: 0,
            lastActivityAt: OBSERVED_AT,
          },
        ],
      }),
    );

    render(<WorkspaceRuntimeFootprintCard workspaceId={WORKSPACE_ID} load={load} />);

    await waitFor(() => expect(screen.getByTestId("runtime-footprint-runtimes")).toBeTruthy());
    expect(load).toHaveBeenCalledWith(WORKSPACE_ID);
    expect(screen.getByTestId("runtime-footprint-processes").textContent).toBe("6");
    expect(screen.getByTestId("runtime-footprint-committed").textContent).toBe("400.0 MB");
    expect(screen.getByTestId("runtime-footprint-resident").textContent).toBe("300.0 MB");
    expect(screen.getByTestId("runtime-footprint-rows").textContent).toContain("session-1");
    expect(screen.getByTestId("runtime-footprint-rows").textContent).toContain("session-2");
  });

  it("reports an unmeasured observation instead of showing zero memory in use", async () => {
    const load = vi.fn().mockResolvedValue(footprint({ complete: false, workspaceRuntimes: 0 }));

    render(<WorkspaceRuntimeFootprintCard workspaceId={WORKSPACE_ID} load={load} />);

    await waitFor(() => expect(screen.getByTestId("runtime-footprint-unmeasured")).toBeTruthy());
    expect(screen.queryByTestId("runtime-footprint-resident")).not.toBeTruthy();
  });

  it("marks a partial measurement as a lower bound rather than a total", async () => {
    const load = vi.fn().mockResolvedValue(
      footprint({
        unreadableRuntimes: 1,
        residentBytes: 1024 * 1024,
        runtimes: [
          {
            sessionId: "session-1",
            taskId: "task-1",
            processes: 3,
            committedBytes: 0,
            residentBytes: 1024 * 1024,
            unreadableProcesses: 2,
            lastActivityAt: OBSERVED_AT,
          },
        ],
      }),
    );

    render(<WorkspaceRuntimeFootprintCard workspaceId={WORKSPACE_ID} load={load} />);

    await waitFor(() => expect(screen.getByTestId("runtime-footprint-partial")).toBeTruthy());
  });

  it("does not invent a committed figure on a platform that does not report one", async () => {
    const load = vi
      .fn()
      .mockResolvedValue(footprint({ committedBytes: 0, residentBytes: 1024 * 1024 * 300 }));

    render(<WorkspaceRuntimeFootprintCard workspaceId={WORKSPACE_ID} load={load} />);

    await waitFor(() => expect(screen.getByTestId("runtime-footprint-committed")).toBeTruthy());
    // The committed figure must not be derived from resident bytes: this platform
    // reported no commit charge, so the card says so instead of inventing a number.
    expect(screen.getByTestId("runtime-footprint-committed").textContent).not.toContain("MB");
    expect(screen.getByTestId("runtime-footprint-resident").textContent).toBe("300.0 MB");
  });

  it("reports a failed read rather than an empty measurement", async () => {
    const load = vi.fn().mockRejectedValue(new Error("backend unavailable"));

    render(<WorkspaceRuntimeFootprintCard workspaceId={WORKSPACE_ID} load={load} />);

    await waitFor(() => expect(screen.getByTestId("runtime-footprint-error")).toBeTruthy());
  });

  it("reports a workspace with no live runtimes", async () => {
    const load = vi.fn().mockResolvedValue(footprint({ workspaceRuntimes: 0, liveRuntimes: 2 }));

    render(<WorkspaceRuntimeFootprintCard workspaceId={WORKSPACE_ID} load={load} />);

    await waitFor(() => expect(screen.getByTestId("runtime-footprint-empty")).toBeTruthy());
  });
});
