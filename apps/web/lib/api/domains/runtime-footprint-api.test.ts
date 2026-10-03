import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/config", () => ({
  getBackendConfig: () => ({ apiBaseUrl: "http://api.test" }),
}));
import {
  fetchWorkspaceRuntimeFootprint,
  isFootprintPartial,
  reportsCommittedBytes,
  type WorkspaceRuntimeFootprint,
} from "./runtime-footprint-api";

const ENCODED_PATH = "http://api.test/api/v1/workspaces/a%2Fb%3Fc%3Dd/runtime-footprint";

const OBSERVED_AT = "2026-10-03T00:00:00Z";
const RESIDENT_800 = 800;

function footprint(overrides: Partial<WorkspaceRuntimeFootprint> = {}): WorkspaceRuntimeFootprint {
  return {
    liveRuntimes: 0,
    workspaceRuntimes: 0,
    processes: 0,
    committedBytes: 0,
    residentBytes: 0,
    unreadableRuntimes: 0,
    complete: true,
    observedAt: OBSERVED_AT,
    runtimes: [],
    ...overrides,
  };
}

describe("fetchWorkspaceRuntimeFootprint", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reads the workspace-scoped path and maps the wire shape", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          live_runtimes: 5,
          workspace_runtimes: 2,
          processes: 9,
          committed_bytes: 1300,
          resident_bytes: 800,
          unreadable_runtimes: 1,
          complete: true,
          observed_at: OBSERVED_AT,
          runtimes: [
            {
              session_id: "session-1",
              task_id: "task-1",
              status: "running",
              processes: 4,
              committed_bytes: 1000,
              resident_bytes: 600,
              unreadable_processes: 1,
              last_activity_at: "2026-10-03T00:00:00Z",
            },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await fetchWorkspaceRuntimeFootprint("workspace-a");

    expect(fetchMock).toHaveBeenCalledWith(
      ENCODED_PATH.replace("a%2Fb%3Fc%3Dd", "workspace-a"),
      expect.anything(),
    );
    expect(result.workspaceRuntimes).toBe(2);
    expect(result.runtimes[0].sessionId).toBe("session-1");
    expect(result.runtimes[0].unreadableProcesses).toBe(1);
  });

  it("encodes a workspace identity so it cannot break out of the path", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(footprint()), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await fetchWorkspaceRuntimeFootprint("a/b?c=d");

    expect(fetchMock.mock.calls[0][0]).toBe(ENCODED_PATH);
  });

  it("reports an incomplete observation rather than an empty one", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          live_runtimes: 0,
          workspace_runtimes: 0,
          processes: 0,
          committed_bytes: 0,
          resident_bytes: 0,
          unreadable_runtimes: 0,
          complete: false,
          observed_at: OBSERVED_AT,
          runtimes: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await fetchWorkspaceRuntimeFootprint("workspace-a");

    expect(result.complete).toBe(false);
    expect(result.runtimes).toHaveLength(0);
  });
});

describe("isFootprintPartial", () => {
  it("is false for a fully measured observation", () => {
    expect(isFootprintPartial(footprint({ unreadableRuntimes: 0 }))).toBe(false);
  });

  it("is true when any runtime could not be fully measured", () => {
    expect(isFootprintPartial(footprint({ unreadableRuntimes: 1 }))).toBe(true);
  });
});

describe("reportsCommittedBytes", () => {
  it("is true when the platform reported commit charge", () => {
    expect(reportsCommittedBytes(footprint({ committedBytes: 1300 }))).toBe(true);
  });

  it("is false on a platform that does not report commit charge", () => {
    expect(
      reportsCommittedBytes(footprint({ committedBytes: 0, residentBytes: RESIDENT_800 })),
    ).toBe(false);
  });
});
