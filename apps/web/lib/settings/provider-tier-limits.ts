import type { BillingTier } from "@/lib/api/domains/provider-limits-api";

export const BILLING_TIERS: BillingTier[] = ["free", "subscription", "paid"];
export const TIER_LABELS = {
  free: "agents:providerTierFree",
  subscription: "agents:providerTierSubscription",
  paid: "agents:providerTierPaid",
};
