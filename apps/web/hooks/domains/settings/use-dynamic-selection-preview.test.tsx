import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DynamicPreviewResponse } from "@/lib/api/domains/dynamic-preview-api";

const { preview } = vi.hoisted(() => ({ preview: vi.fn() }));
vi.mock("@/lib/api/domains/dynamic-preview-api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/domains/dynamic-preview-api")>()),
  previewDynamicProfile: preview,
}));

import { useDynamicSelectionPreview } from "./use-dynamic-selection-preview";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, resolve, reject };
}

function readyPreview(candidateId: string): DynamicPreviewResponse {
  return {
    state: "ready",
    candidate_id: candidateId,
    observed_at: "2026-10-03T00:00:00Z",
    considered: [],
    usage_complete: true,
  };
}

describe("useDynamicSelectionPreview", () => {
  beforeEach(() => preview.mockReset());

  it("applies the response that matches the current draft revision", async () => {
    const first = deferred<DynamicPreviewResponse>();
    const second = deferred<DynamicPreviewResponse>();
    preview.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const view = renderHook(
      ({ revision }) => useDynamicSelectionPreview({ payload: {}, revision, enabled: true }),
      { initialProps: { revision: "r1" } },
    );
    expect(view.result.current.state).toEqual({ status: "loading" });

    view.rerender({ revision: "r2" });
    await act(async () => {
      first.resolve(readyPreview("stale"));
      await first.promise;
    });
    // The older response must not land under the newer draft.
    expect(view.result.current.state).toEqual({ status: "loading" });

    await act(async () => {
      second.resolve(readyPreview("current"));
      await second.promise;
    });
    await waitFor(() => expect(view.result.current.state.status).toBe("ready"));
    expect(view.result.current.state).toMatchObject({
      preview: { candidate_id: "current" },
    });
  });

  it("reports a read failure without clearing an earlier prediction", async () => {
    preview.mockResolvedValueOnce(readyPreview("candidate-a"));
    const view = renderHook(() =>
      useDynamicSelectionPreview({ payload: {}, revision: "r1", enabled: true }),
    );
    await waitFor(() => expect(view.result.current.state.status).toBe("ready"));

    preview.mockRejectedValueOnce(new Error("preview read failed"));
    await act(async () => view.result.current.refresh());
    await waitFor(() => expect(view.result.current.state).toEqual({ status: "failed" }));
  });

  it("stays idle and issues no request when the preview is disabled", async () => {
    preview.mockResolvedValue(readyPreview("candidate-a"));
    const view = renderHook(() =>
      useDynamicSelectionPreview({ payload: {}, revision: "r1", enabled: false }),
    );
    expect(view.result.current.state).toEqual({ status: "idle" });
    expect(preview).not.toHaveBeenCalled();
  });

  it("re-requests on refresh for the same revision", async () => {
    preview.mockResolvedValue(readyPreview("candidate-a"));
    const view = renderHook(() =>
      useDynamicSelectionPreview({ payload: {}, revision: "r1", enabled: true }),
    );
    await waitFor(() => expect(view.result.current.state.status).toBe("ready"));
    await act(async () => view.result.current.refresh());
    await waitFor(() => expect(preview).toHaveBeenCalledTimes(2));
  });
});
