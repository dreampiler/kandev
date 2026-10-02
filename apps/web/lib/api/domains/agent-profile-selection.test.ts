import { describe, it, expect } from "vitest";
import { normalizeAgentProfile, toAgentProfilePayload } from "./agent-profile-normalize";

const dynamicProfileId = "dynamic-tier-profile";

const basePolicy = {
  version: 1,
  transient: {
    retry: { enabled: true, max_retries: 2, initial_interval_seconds: 5 },
    wait_for_reset: { enabled: true, max_wait_seconds: 300 },
    on_exhausted: "skip",
  },
  hard: {
    retry: { enabled: false, max_retries: 0, initial_interval_seconds: 0 },
    wait_for_reset: { enabled: false, max_wait_seconds: 0 },
    on_exhausted: "stop",
  },
  unclassified: { enabled: false, consecutive_failure_threshold: 0 },
};

function dynamicPayload(
  candidates: Record<string, unknown>[],
  extra: Record<string, unknown> = {},
) {
  return {
    id: dynamicProfileId,
    kind: "dynamic",
    dynamic: { version: 1, candidates, ...extra },
  };
}

describe("dynamic tier selection normalization", () => {
  it("reads a joined tier with head policy, model options and a manual window", () => {
    const result = normalizeAgentProfile(
      dynamicPayload([
        {
          position: 0,
          execution_profile_id: "head",
          enabled: true,
          policies: {
            ...basePolicy,
            selection: {
              join_previous: false,
              tier: { mode: "pace", on_failure: "same_tier_next" },
              model: {
                cost: "subscription",
                usage_source: "manual",
                reserved_user_share_pct: 25,
                windows: [
                  {
                    period: "month",
                    unit: "money",
                    limit: "120.50",
                    reset: { anchor: "09:00", timezone: "Asia/Seoul" },
                  },
                ],
              },
            },
          },
        },
        {
          position: 1,
          execution_profile_id: "joined",
          enabled: true,
          policies: {
            ...basePolicy,
            selection: {
              join_previous: true,
              model: { cost: "", usage_source: "none", reserved_user_share_pct: 0 },
            },
          },
        },
      ]),
    );

    const head = result.dynamic?.candidates[0]?.policies.selection;
    expect(head?.joinPrevious).toBe(false);
    expect(head?.tier).toEqual({ mode: "pace", onFailure: "same_tier_next" });
    expect(head?.model).toEqual({
      cost: "subscription",
      usageSource: "manual",
      reservedUserSharePct: 25,
      windows: [
        {
          period: "month",
          unit: "money",
          limit: "120.50",
          reset: { anchor: "09:00", timezone: "Asia/Seoul" },
        },
      ],
    });

    const joined = result.dynamic?.candidates[1]?.policies.selection;
    expect(joined?.joinPrevious).toBe(true);
    // Only a tier head carries a tier; a joined row must not invent one.
    expect(joined?.tier).toBeUndefined();
  });

  it("leaves a legacy row unconfigured instead of manufacturing defaults", () => {
    const result = normalizeAgentProfile(
      dynamicPayload([
        { position: 0, execution_profile_id: "legacy", enabled: true, policies: basePolicy },
      ]),
    );
    expect(result.dynamic?.candidates[0]?.policies.selection).toBeUndefined();
  });

  it("drops a tier with an unknown mode rather than passing it through", () => {
    const result = normalizeAgentProfile(
      dynamicPayload([
        {
          position: 0,
          execution_profile_id: "head",
          enabled: true,
          policies: {
            ...basePolicy,
            selection: {
              join_previous: false,
              tier: { mode: "cheapest", on_failure: "same_tier_next" },
              model: { cost: "free", usage_source: "none", reserved_user_share_pct: 0 },
            },
          },
        },
      ]),
    );
    expect(result.dynamic?.candidates[0]?.policies.selection?.tier).toBeUndefined();
  });
});

describe("dynamic tier selection save payload", () => {
  it("omits selection from the write payload when the draft has none", () => {
    const profile = normalizeAgentProfile(
      dynamicPayload([
        { position: 0, execution_profile_id: "legacy", enabled: true, policies: basePolicy },
      ]),
    );
    const payload = toAgentProfilePayload(profile);
    const policy = (payload.dynamic as { candidates: { policies: Record<string, unknown> }[] })
      .candidates[0]?.policies;
    // A defaulted object here would erase the server's saved configuration.
    expect(policy).not.toHaveProperty("selection");
  });

  it("round-trips selection and the keep-model preference through save", () => {
    const profile = normalizeAgentProfile(
      dynamicPayload(
        [
          {
            position: 0,
            execution_profile_id: "head",
            enabled: true,
            policies: {
              ...basePolicy,
              selection: {
                join_previous: false,
                tier: { mode: "cost", on_failure: "next_tier" },
                model: {
                  cost: "metered",
                  usage_source: "automatic",
                  reserved_user_share_pct: 10,
                },
              },
            },
          },
        ],
        { keep_model_while_running: false },
      ),
    );
    expect(profile.dynamic?.keepModelWhileRunning).toBe(false);

    const payload = toAgentProfilePayload(profile) as {
      dynamic: {
        keep_model_while_running?: boolean;
        candidates: { policies: { selection?: Record<string, unknown> } }[];
      };
    };
    expect(payload.dynamic.keep_model_while_running).toBe(false);
    // The server's tag is `on_failure`. A verbatim camelCase spread of the tier
    // object does not decode, so the stored direction silently fell back to the
    // default while this assertion still passed.
    expect(payload.dynamic.candidates[0]?.policies.selection).toEqual({
      join_previous: false,
      tier: { mode: "cost", on_failure: "next_tier" },
      model: { cost: "metered", usage_source: "automatic", reserved_user_share_pct: 10 },
    });
  });

  it("omits the keep-model field when the request never set it", () => {
    const profile = normalizeAgentProfile(
      dynamicPayload([
        { position: 0, execution_profile_id: "legacy", enabled: true, policies: basePolicy },
      ]),
    );
    const payload = toAgentProfilePayload(profile) as { dynamic: Record<string, unknown> };
    expect(payload.dynamic).not.toHaveProperty("keep_model_while_running");
  });
});
