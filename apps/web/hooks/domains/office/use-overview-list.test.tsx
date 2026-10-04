import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useOverviewList } from "./use-overview-list";

const page = { kind: "tasks" as const, total: 1, tasks: [] };

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
}

describe("useOverviewList", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    setVisibility("visible");
    vi.useRealTimers();
  });

  it("makes no request while the list is closed", async () => {
    const load = vi.fn().mockResolvedValue(page);
    const { result } = renderHook(() => useOverviewList("k", null));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(load).not.toHaveBeenCalled();
    expect(result.current.loadState).toBe("idle");
  });

  it("loads when opened and refreshes only while visible", async () => {
    const load = vi.fn().mockResolvedValue(page);
    const { result } = renderHook(() => useOverviewList("k", load));
    await act(async () => {
      await Promise.resolve();
    });
    expect(load).toHaveBeenCalledTimes(1);
    expect(result.current.loadState).toBe("loaded");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(load).toHaveBeenCalledTimes(2);

    setVisibility("hidden");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(180_000);
    });
    expect(load).toHaveBeenCalledTimes(2);
  });

  it("reloads at once for a new key and drops rows when closed", async () => {
    const load = vi.fn().mockResolvedValue(page);
    const { result, rerender } = renderHook(
      ({ key, open }: { key: string; open: boolean }) => useOverviewList(key, open ? load : null),
      { initialProps: { key: "a", open: true } },
    );
    await act(async () => {
      await Promise.resolve();
    });
    rerender({ key: "b", open: true });
    await act(async () => {
      await Promise.resolve();
    });
    expect(load).toHaveBeenCalledTimes(2);

    rerender({ key: "b", open: false });
    expect(result.current).toEqual({ data: null, loadState: "idle" });
  });
});
