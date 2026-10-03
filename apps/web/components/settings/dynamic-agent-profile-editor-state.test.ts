import { describe, expect, it } from "vitest";
import type { DynamicAgentCandidate, DynamicAgentPolicy } from "@/lib/types/agent-profile";
import { agentProfileId } from "@/lib/types/ids";
import * as editorState from "./dynamic-agent-profile-editor-state";
import { dynamicDraftRevision } from "./dynamic-agent-profile-editor-draft";

const PROFILE_NAME = "Dynamic";
const CANDIDATE_ID = "candidate-1";

function defaultPolicy(): DynamicAgentPolicy {
  return {
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
    unclassified: { enabled: false, consecutiveFailureThreshold: 0 },
  };
}

function candidate(id: string, policies: Partial<DynamicAgentPolicy> = {}): DynamicAgentCandidate {
  return {
    position: 0,
    executionProfileId: agentProfileId(id),
    enabled: true,
    policies: { ...defaultPolicy(), ...policies },
  };
}

describe("dynamic profile editor save payload", () => {
  it("retains API-configured unclassified policy values", () => {
    const payload = editorState.dynamicProfilePayload(PROFILE_NAME, true, 1, [
      candidate(CANDIDATE_ID, {
        unclassified: { enabled: true, consecutiveFailureThreshold: 4 },
      }),
    ]);

    expect(payload).toMatchObject({
      dynamic: {
        candidates: [
          { policies: { unclassified: { enabled: true, consecutive_failure_threshold: 4 } } },
        ],
      },
    });
  });

  it("carries the keep-model preference and tier selection into the saved document", () => {
    const payload = editorState.dynamicProfilePayload(
      "Dynamic",
      true,
      1,
      [
        candidate(CANDIDATE_ID, {
          selection: {
            joinPrevious: false,
            tier: { mode: "pace", onFailure: "next_tier" },
            model: { cost: "subscription", usageSource: "automatic", reservedUserSharePct: 25 },
          },
        }),
      ],
      false,
    );

    expect(payload.dynamic.keep_model_while_running).toBe(false);
    expect(payload.dynamic.candidates[0].policies.selection).toEqual({
      join_previous: false,
      tier: { mode: "pace", on_failure: "next_tier" },
      model: { cost: "subscription", usage_source: "automatic", reserved_user_share_pct: 25 },
    });
  });

  it("omits a row's selection entirely when the draft never set one", () => {
    const payload = editorState.dynamicProfilePayload(PROFILE_NAME, true, 1, [
      candidate(CANDIDATE_ID),
    ]);
    expect(payload.dynamic.candidates[0].policies).not.toHaveProperty("selection");
  });

  it("defaults keep-model on when the caller supplies no preference", () => {
    const payload = editorState.dynamicProfilePayload(PROFILE_NAME, true, 1, [
      candidate(CANDIDATE_ID),
    ]);
    expect(payload.dynamic.keep_model_while_running).toBe(true);
  });
});

describe("dynamicDraftRevision", () => {
  it("changes when only the keep-model preference changes", () => {
    const base = dynamicDraftRevision(PROFILE_NAME, [candidate("a")], true, true);
    expect(dynamicDraftRevision(PROFILE_NAME, [candidate("a")], true, false)).not.toBe(base);
  });

  it("is stable for identical drafts", () => {
    expect(dynamicDraftRevision(PROFILE_NAME, [candidate("a")], true, true)).toBe(
      dynamicDraftRevision(PROFILE_NAME, [candidate("a")], true, true),
    );
  });
});
