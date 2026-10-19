import { beforeEach, describe, expect, it, vi } from "vitest";
import { fetchJson } from "../client";
import {
  getControlOverview,
  getControlWorkspaceOverview,
  getControlWorkspaceTasks,
} from "./control-overview-api";

vi.mock("../client", () => ({ fetchJson: vi.fn().mockResolvedValue({}) }));

describe("integrated control read scope", () => {
  beforeEach(() => {
    vi.mocked(fetchJson).mockClear();
  });

  it("requests reachable workspaces explicitly for aggregate history", async () => {
    await getControlOverview();
    expect(fetchJson).toHaveBeenCalledWith(
      "/api/v1/office/workspaces/aggregate?window_hours=24&scope=reachable",
      undefined,
    );
  });

  it("preserves workspace identity, scope and cancellation on detail reads", async () => {
    const options = { init: { signal: new AbortController().signal } };
    await getControlWorkspaceOverview("workspace & one", options);
    expect(fetchJson).toHaveBeenCalledWith(
      "/api/v1/office/workspaces/aggregate/workspace?workspace_id=workspace+%26+one&window_hours=24&scope=reachable",
      options,
    );
  });

  it("retains the exact task filter and full-list intent within reachable scope", async () => {
    await getControlWorkspaceTasks("workspace", "problems", -1);
    expect(fetchJson).toHaveBeenCalledWith(
      "/api/v1/office/workspaces/aggregate/tasks?workspace_id=workspace&filter=problems&limit=-1&scope=reachable",
      undefined,
    );
  });
});
