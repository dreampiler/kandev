import { describe, expect, it } from "vitest";
import type { LimitBudget, ProviderAccountLimit } from "@/lib/api/domains/provider-limits-api";
import { getProviderTierState, updateBudgetOverride } from "./provider-tier-limits";

const budget: LimitBudget = {
  window: "5h",
  unit: "tokens",
  reset: "rolling",
  value: 100,
  scope: "premium",
};

describe("provider tier inheritance", () => {
  it("keeps a blank allowance inherited while editing unit or reset", () => {
    const windows = updateBudgetOverride([], budget, { unit: "usd" });
    expect(windows).toEqual([{ window: "5h", scope: "premium", unit: "usd", reset: "rolling" }]);
    expect(updateBudgetOverride(windows, budget, { reset: "utc" })[0].value).toBeUndefined();
  });

  it("keeps explicit zero and clears only the edited scope", () => {
    const sibling = { ...budget, scope: "other", value: 20 };
    const windows = updateBudgetOverride([sibling], budget, { value: 0 });
    expect(windows.find((entry) => entry.scope === "premium")?.value).toBe(0);
    expect(updateBudgetOverride(windows, budget, {}, true)).toEqual([sibling]);
  });

  it("provides an editable empty state for a missing tier", () => {
    const account = { tiers: {} } as ProviderAccountLimit;
    expect(getProviderTierState(account, "paid")).toEqual({
      defaults: {},
      learned: {},
      override: {},
      effective: {},
      usage: {},
    });
  });
});
