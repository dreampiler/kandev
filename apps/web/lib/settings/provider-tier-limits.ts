import type {
  BillingTier,
  LimitBudget,
  ProviderAccountLimit,
} from "@/lib/api/domains/provider-limits-api";

export const BILLING_TIERS: BillingTier[] = ["free", "subscription", "paid"];
export const TIER_LABELS = {
  free: "agents:providerTierFree",
  subscription: "agents:providerTierSubscription",
  paid: "agents:providerTierPaid",
};

export function getProviderTierState(account: ProviderAccountLimit, tier: BillingTier) {
  return (
    account.tiers[tier] ?? { defaults: {}, learned: {}, override: {}, effective: {}, usage: {} }
  );
}

export function budgetKey(budget: LimitBudget) {
  return `${budget.window}:${budget.scope ?? ""}`;
}

export function updateBudgetOverride(
  overrides: LimitBudget[],
  budget: LimitBudget,
  patch: Partial<LimitBudget>,
  clear = false,
) {
  const previous = overrides.find((entry) => budgetKey(entry) === budgetKey(budget));
  const windows = overrides.filter((entry) => budgetKey(entry) !== budgetKey(budget));
  if (!clear)
    windows.push({
      window: budget.window,
      scope: budget.scope,
      unit: budget.unit,
      reset: budget.reset,
      reset_at: budget.reset_at,
      timezone: budget.timezone,
      ...previous,
      ...patch,
    });
  return windows;
}
