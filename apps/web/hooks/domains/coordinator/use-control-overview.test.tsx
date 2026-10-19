import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useControlOverview } from "./use-control-overview";

const fixture = vi.hoisted(() => ({ aggregate: vi.fn(), workspace: vi.fn() }));
vi.mock("@/components/state-provider", () => ({ useAppStore: () => "owner" }));
vi.mock("@/lib/api/domains/control-overview-api", () => ({
  getControlOverview: fixture.aggregate,
  getControlWorkspaceOverview: fixture.workspace,
  getControlWorkspaceTasks: vi.fn(),
}));
afterEach(cleanup);

describe("history workspace request identity", () => {
  it("narrows the read and discards late history from the previous workspace", async () => {
    const pending: Array<(value: { workspace: string }) => void> = [];
    fixture.workspace.mockImplementation(() => new Promise((resolve) => pending.push(resolve)));
    const revision = {};
    const view = renderHook(({ id }) => useControlOverview(revision, id), {
      initialProps: { id: "w1" },
    });
    expect(fixture.workspace.mock.calls[0][0]).toBe("w1");
    view.rerender({ id: "w2" });
    expect(fixture.workspace.mock.calls[0][1].init.signal.aborted).toBe(true);
    await act(async () => pending[0]({ workspace: "w1" }));
    expect(view.result.current.data).toBeUndefined();
    await act(async () => pending[1]({ workspace: "w2" }));
    await waitFor(() => expect(view.result.current.data).toEqual({ workspace: "w2" }));
    expect(fixture.aggregate).not.toHaveBeenCalled();
  });
});
