import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import {
  listResourceCircuits,
  releaseResourceCircuit,
} from "@/lib/api/domains/resource-circuit-api";
import { useResourceCircuits } from "./use-resource-circuits";

vi.mock("@/lib/api/domains/resource-circuit-api", () => ({
  listResourceCircuits: vi.fn(),
  releaseResourceCircuit: vi.fn(),
}));

const one = {
  resource_key: "model:one",
  state: "open",
  code: "quota_limited",
  until: "2026-10-14T00:00:00Z",
  strikes: 1,
};
const sibling = { ...one, resource_key: "account_block:sibling", code: "account_blocked" };

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(listResourceCircuits).mockResolvedValue({ circuits: [one, sibling] });
  vi.mocked(releaseResourceCircuit).mockResolvedValue({ released: true });
});

it("releases only the selected circuit and keeps its sibling", async () => {
  const { result } = renderHook(() => useResourceCircuits());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(() => result.current.release(one.resource_key));
  expect(releaseResourceCircuit).toHaveBeenCalledWith("model:one");
  expect(result.current.circuits).toEqual([sibling]);
  expect(result.current.releasing).toBeNull();
});

it("retains a failed release and permits a retry", async () => {
  vi.mocked(releaseResourceCircuit).mockRejectedValueOnce(new Error("save failed"));
  const { result } = renderHook(() => useResourceCircuits());
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(() => result.current.release(one.resource_key));
  expect(result.current.error).toBe(true);
  expect(result.current.circuits).toEqual([one, sibling]);
  expect(result.current.releasing).toBeNull();
  await act(() => result.current.release(one.resource_key));
  expect(result.current.error).toBe(false);
  expect(result.current.circuits).toEqual([sibling]);
});

it("serializes same-tick releases and ignores a stale list response", async () => {
  let resolve!: (value: { circuits: (typeof one)[] }) => void;
  vi.mocked(listResourceCircuits).mockReturnValue(
    new Promise((done) => {
      resolve = done;
    }),
  );
  const { result } = renderHook(() => useResourceCircuits());
  await act(async () => {
    await Promise.all([
      result.current.release(one.resource_key),
      result.current.release(one.resource_key),
    ]);
  });
  expect(releaseResourceCircuit).toHaveBeenCalledTimes(1);
  await act(async () => resolve({ circuits: [one] }));
  expect(result.current.circuits).toEqual([]);
  expect(result.current.loading).toBe(false);
  expect(result.current.releasing).toBeNull();
});

it("reports a list failure instead of treating it as a healthy empty list", async () => {
  vi.mocked(listResourceCircuits).mockRejectedValue(new Error("unavailable"));
  const { result } = renderHook(() => useResourceCircuits());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.error).toBe(true);
});
