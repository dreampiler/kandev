import { describe, it, expect } from "vitest";
import { normalizeAgentProfile, toAgentProfilePayload } from "./agent-profile-normalize";

function profileWithWindow(window: Record<string, unknown>) {
  return {
    id: "dynamic-scope-profile",
    kind: "dynamic",
    dynamic: {
      version: 1,
      candidates: [
        {
          position: 0,
          execution_profile_id: "zen-free",
          enabled: true,
          policies: {
            version: 1,
            selection: {
              join_previous: false,
              tier: { mode: "pace", on_failure: "same_tier_next" },
              model: {
                cost: "free",
                usage_source: "manual",
                reserved_user_share_pct: 0,
                windows: [window],
              },
            },
          },
        },
      ],
    },
  };
}

const dailyTokens = {
  period: "day",
  unit: "tokens",
  limit: "50000000",
  reset: { anchor: "00:00", timezone: "UTC" },
};

type WindowPayload = {
  dynamic: {
    candidates: { policies: { selection: { model: { windows: Record<string, unknown>[] } } } }[];
  };
};

describe("manual window account scope", () => {
  it("round-trips an account-scoped window through read and save", () => {
    const profile = normalizeAgentProfile(profileWithWindow({ ...dailyTokens, scope: "account" }));
    const model = profile.dynamic?.candidates[0]?.policies.selection?.model;
    expect(model?.windows?.[0]?.scope).toBe("account");

    const payload = toAgentProfilePayload(profile) as unknown as WindowPayload;
    expect(payload.dynamic.candidates[0]?.policies.selection.model.windows[0]).toEqual({
      ...dailyTokens,
      scope: "account",
    });
  });

  it("keeps a candidate window without a scope field and drops unknown scopes", () => {
    for (const scope of [undefined, "candidate", "workspace"]) {
      const profile = normalizeAgentProfile(profileWithWindow({ ...dailyTokens, scope }));
      const window = profile.dynamic?.candidates[0]?.policies.selection?.model.windows?.[0];
      expect(window?.scope).toBeUndefined();
      const payload = toAgentProfilePayload(profile) as unknown as WindowPayload;
      expect(payload.dynamic.candidates[0]?.policies.selection.model.windows[0]).toEqual(
        dailyTokens,
      );
    }
  });
});
