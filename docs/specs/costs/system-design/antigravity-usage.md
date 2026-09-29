---
status: draft
system: costs
created: 2026-09-29
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-001
---

# Antigravity subscription usage

## Boundary

`AntigravityACP.BillingType` marks Antigravity profiles as subscriptions. The
existing `usageProviderAdapter` registers `AntigravityUsageClient` for those
profiles, and `UsageService` caches its response using the same five-minute
policy as Claude and Codex. Existing Office usage projections consume the
shared `ProviderUsage` shape; no new API or frontend format is needed.

## Quota read

The client runs `agy -p "/usage" --output-format json` with a bounded context.
This local usage command produces zero model turns and zero tokens in the
observed CLI response. The client selects the `Gemini Models` group from
`command.data.groups[]`; the separate Claude/GPT group is never attributed to
Gemini. Each bucket with a fraction from zero to one and a valid UTC reset
instant becomes a `UtilizationWindow`. The label follows the existing `5-hour`
or `7-day` convention, and `UtilizationPct` is
`100 * (1 - remaining_fraction)`. A bucket without a valid reset instant is
omitted because it cannot describe an active window.

The CLI is run as the backend host user and reads its own account state. Kandev
does not read or persist Antigravity credentials. Missing CLI, failed command,
invalid JSON, and an absent active Gemini window return a usage fetch error;
the client never substitutes a different model group's quota.

## Requirement mapping

| Requirement | Design outcome |
| --- | --- |
| AC-COSTS-SUBSCRIPTION-USAGE-001.9 | Register the subscription client and select only `Gemini Models`. |
| AC-COSTS-SUBSCRIPTION-USAGE-001.10 | Convert active buckets to labeled utilization windows with reset times. |
