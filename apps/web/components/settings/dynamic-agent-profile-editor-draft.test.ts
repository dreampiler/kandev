import { cleanup, renderHook, act } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentProfile } from "@/lib/types/http";
import type { DynamicAgentCandidate } from "@/lib/types/agent-profile";
import { useDynamicAgentProfileEditorDraft } from "./dynamic-agent-profile-editor-draft";

vi.mock("@/hooks/domains/features/use-feature", () => ({
  useFeature: () => false,
}));

const unclassifiedOff = { enabled: false, consecutiveFailureThreshold: 0 } as const;

function candidate(id: string, threshold = 0): DynamicAgentCandidate {
  return {
    position: 0,
    executionProfileId: id as DynamicAgentCandidate["executionProfileId"],
    enabled: true,
    policies: {
      version: 1,
      transient: {
        retry: { enabled: false, maxRetries: 0, initialIntervalSeconds: 0 },
        waitForReset: { enabled: false, maxWaitSeconds: 0 },
        onExhausted: "skip",
      },
      hard: {
        retry: { enabled: false, maxRetries: 0, initialIntervalSeconds: 0 },
        waitForReset: { enabled: false, maxWaitSeconds: 0 },
        onExhausted: "stop",
      },
      unclassified: { enabled: threshold > 0, consecutiveFailureThreshold: threshold },
    },
  };
}

function profile(candidates: DynamicAgentCandidate[]): AgentProfile {
  return {
    id: "profile-1",
    agentId: "agent-1",
    name: "Dynamic",
    enabled: true,
    dynamic: { version: 1, candidates },
  } as unknown as AgentProfile;
}

describe("unclassified draft mutations", () => {
  afterEach(cleanup);

  it("updates only the targeted candidate's unclassified policy", () => {
    const onDraftChange = vi.fn();
    const { result } = renderHook(() =>
      useDynamicAgentProfileEditorDraft({
        profile: profile([candidate("a"), candidate("b")]),
        onDraftChange,
      }),
    );

    act(() => {
      result.current.updateCandidateUnclassified(1, {
        enabled: true,
        consecutiveFailureThreshold: 3,
      });
    });

    const [first, second] = result.current.candidates;
    expect(first.policies.unclassified).toMatchObject(unclassifiedOff);
    expect(second.policies.unclassified).toMatchObject({
      enabled: true,
      consecutiveFailureThreshold: 3,
    });
    expect(second.policies.transient.onExhausted).toBe("skip");
    expect(onDraftChange).toHaveBeenCalled();
  });

  it("applies enabled threshold 2 to every candidate without touching other policies", () => {
    const onDraftChange = vi.fn();
    const { result } = renderHook(() =>
      useDynamicAgentProfileEditorDraft({
        profile: profile([candidate("a", 5), candidate("b", 7)]),
        onDraftChange,
      }),
    );

    act(() => {
      result.current.applyUnclassifiedToAllCandidates();
    });

    for (const current of result.current.candidates) {
      expect(current.policies.unclassified).toEqual({
        enabled: true,
        consecutiveFailureThreshold: 2,
      });
      expect(current.policies.hard.onExhausted).toBe("stop");
    }
    expect(result.current.candidates.map((current) => current.executionProfileId)).toEqual([
      "a",
      "b",
    ]);
  });
});
