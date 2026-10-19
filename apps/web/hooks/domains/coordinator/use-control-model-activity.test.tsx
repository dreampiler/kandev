import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useControlModelActivity } from "./use-control-provider-usage";

const fixture = vi.hoisted(() => ({ readModels: vi.fn() }));
vi.mock("@/components/state-provider", () => ({ useAppStore: () => "owner" }));
vi.mock("@/lib/api/domains/stats-api", () => ({ fetchModelUsage: fixture.readModels }));

beforeEach(() => {
  fixture.readModels.mockReset();
});

describe("recorded model statistics contract", () => {
  it.each([
    { invalid: { models: [] } },
    { invalid: [{ model: "sol", session_count: -1, turn_count: 0, total_duration_ms: 0 }] },
  ])("reports incompatible data as unknown rather than ready", async ({ invalid }) => {
    fixture.readModels.mockResolvedValue(invalid);
    const revision = {};
    const { result } = renderHook(() => useControlModelActivity(["w1"], revision));
    await waitFor(() => expect(result.current.data?.[0]?.status).toBe("error"));
    expect(result.current.data?.[0]?.models).toEqual([]);
  });

  it("preserves measured zero and the existing seven-day range", async () => {
    const measured = [{ model: "sol", session_count: 0, turn_count: 0, total_duration_ms: 0 }];
    fixture.readModels.mockResolvedValue(measured);
    const revision = {};
    const { result } = renderHook(() => useControlModelActivity(["w1"], revision));
    await waitFor(() => expect(result.current.data?.[0]?.status).toBe("ready"));
    expect(result.current.data?.[0]?.models).toEqual(measured);
    expect(fixture.readModels.mock.calls[0][0]).toBe("w1");
    expect(fixture.readModels.mock.calls[0][2]).toBe("week");
  });
});
