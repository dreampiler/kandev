---
status: draft
system: costs
created: 2026-09-29
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-001
  - REQ-COSTS-SUBSCRIPTION-USAGE-002
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

## Global profile read and presentation

The settings profile at `/settings/agents/[agentId]/profiles/[profileId]` is the
entry point for global Antigravity usage. Its saved profile ID is the lookup key;
the quota card is read-only and remains separate from the editable profile draft
and save contributor. It appears for saved `antigravity-acp` profiles regardless
of whether Office has an agent instance for them. Draft profiles have no quota
card.

Expose a read-only `GET /api/v1/agent-profiles/:id/utilization` alongside the
existing agent-settings profile reads. Resolve the saved, non-deleted profile and
confirm its agent type before calling the existing `usageProviderAdapter.GetUsage`.
The adapter continues to register `AntigravityUsageClient` and reuse its
five-minute cache; the route does not run `agy` directly. Return
`{ "utilization": ProviderUsage }` with the same `windows` fields as the Office
route. A missing profile is 404, an unsupported profile returns null, and a
provider read failure returns a generic unavailable error without command
output or account details. The route follows the existing settings read access
policy and makes no configuration or Office state changes.

The profile page requests usage after it resolves the saved profile. It displays
window label, clamped/rounded percent used, and a localized absolute reset date
and time as persistent text. The `reset_at` UTC instant remains unchanged in the
wire response and is formatted in the viewer's locale and timezone. It shows a
loading state while reading and a localized unavailable state with a retry
button on failure or an empty result. The UI must not turn an error into 0%.
The existing Office usage presentation and source remain intact.

On desktop the quota card follows the profile heading before editable settings.
On a phone the same read-only card is a single column in the existing settings
page scroll, with the window label, percentage, and reset text each visible and
no hover disclosure. Retry has a touch target of at least 44px and is
keyboard-accessible. No drawer, second scroll region, or new navigation route is
needed; `mobile-agent-profile-layout.spec.ts` is the nearest shipped settings
layout example. Browser evidence covers a populated window, unavailable/retry,
and phone containment without creating an Office agent.

## Requirement mapping

| Requirement | Design outcome |
| --- | --- |
| AC-COSTS-SUBSCRIPTION-USAGE-001.9 | Register the subscription client and select only `Gemini Models`. |
| AC-COSTS-SUBSCRIPTION-USAGE-001.10 | Convert active buckets to labeled utilization windows with reset times. |
| AC-COSTS-SUBSCRIPTION-USAGE-002.1, .5 | Show visible window, percent, and reset text in the global profile on desktop and phone. |
| AC-COSTS-SUBSCRIPTION-USAGE-002.2, .4 | Resolve the saved profile and use the existing adapter without an Office instance or write. |
| AC-COSTS-SUBSCRIPTION-USAGE-002.3 | Distinguish missing profile from a source failure and show retry without invented values. |
