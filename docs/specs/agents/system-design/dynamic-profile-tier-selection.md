---
status: draft
system: agents
requirements:
  - REQ-AGENTS-TIER-SELECTION-001
  - REQ-AGENTS-TIER-SELECTION-002
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-AGENTS-TIER-SELECTION-004
  - REQ-AGENTS-TIER-SELECTION-005
created: 2026-10-02
owners:
  - kandev
---

# Dynamic Profile Tier Selection System Design

## Boundary and current evidence

Agents owns the profile document, candidate selection and settings interaction.
[Costs](../../costs/README.md) owns usage facts and prices; this design consumes
them without creating a second ledger or Office policy engine. Task storage
continues to persist route generations, attempts and session usage.

The reviewed fork base is `c8d4c923c41bfe2ca73f73605d938c34397254dc`.
Current evidence:

- `agent/settings/models.DynamicAgentRoute` stores position, concrete profile,
  enabled and `RulesJSON`. Controller `profile_crud.go` serializes only
  `candidate.Policies`; `dynamic_policy.go` normalizes version-1 policies and
  legacy action maps. Adding a UI-only field would silently lose it.
- `runtime/dynamic.Engine.selectContext` iterates ordered candidates.
  `applyPolicyFailure` calls it only after `DecisionSkip`, excluding the current
  candidate alone. `firstSelectableUnclassifiedSuccessor` is a separate forward
  path that must consume the same tier selection without widening admission.
- `PolicyStateJSON` survives route persistence, but creating a new route resets
  most of that object. Chain state must be carried explicitly across decisions.
- `agent/usage.UtilizationWindow` currently has label, percent and reset, but
  no numeric duration. Codex receives `limit_window_seconds` and loses it;
  Claude recognizes five-hour/seven-day windows and richer limit categories.
- `backendapp.usageProviderAdapter` registers host-home ACP usage clients and
  proxy clients. Its current host-home fallback is insufficient evidence that
  an arbitrary executor/profile uses the same account.
- `task_usage_events` has `agent_profile_id`, actual `model`, `occurred_at`,
  `tokens_total`, `cost_subcents`, price provenance and completeness.
  `publishPromptUsage` and `publishNativeUsageObservation` populate the logical
  session profile. `task_session_turns` separately stores `execution_profile_id`
  and `route_generation`; join through `turn_id` instead of the current session.
- `DynamicAgentCandidateList` renders up/down actions and two class policies.
  `useDynamicAgentProfileDraft` owns row changes; editor reconciliation and
  profile HTTP normalization must retain additive settings.

This package extends the existing routing ADR's deferred usage/cost feature.
It retains the ADR's ownership and continuation boundaries. Numeric badges are
derived adjacency, not the semantic capability classes prohibited by the old
flat-list specification. For configured tiers this package replaces flat
successor ordering; all other baseline guarantees still apply.

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| REQ-AGENTS-TIER-SELECTION-001 | Row document; Group mutations; Presentation |
| REQ-AGENTS-TIER-SELECTION-002 | Row document; Compatibility; Authorization |
| REQ-AGENTS-TIER-SELECTION-003 | Usage input; Manual accounting; Ranking |
| REQ-AGENTS-TIER-SELECTION-004 | Decision flow; Durable chain; Continuity |
| REQ-AGENTS-TIER-SELECTION-005 | Preview and reasons; Presentation |

## Row document and API

Keep `dynamic_agent_routes.rules_json` as the only route settings document.
Extend its existing version-1 policy object with optional `selection`. Its
presence does not change the error-policy version or repurpose failure enums.
Proposed shape (illustrative values, never a seeded operational configuration):

```json
{
  "version": 1,
  "transient": {"retry": {"enabled": false, "max_retries": 0, "initial_interval_seconds": 0}, "wait_for_reset": {"enabled": false, "max_wait_seconds": 0}, "on_exhausted": "skip"},
  "hard": {"retry": {"enabled": false, "max_retries": 0, "initial_interval_seconds": 0}, "wait_for_reset": {"enabled": false, "max_wait_seconds": 0}, "on_exhausted": "skip"},
  "selection": {
    "join_previous": false,
    "tier": {"mode": "order", "on_failure": "same_tier_next"},
    "model": {"cost": "subscription", "usage_source": "automatic", "reserved_user_share_pct": 0}
  }
}
```

`tier.mode` is `order|pace|cost`; `on_failure` is
`same_tier_next|next_tier`. `model.cost` is `free|subscription|metered` and
is routing metadata, not an edit to authentication `billing_type`.
`usage_source` is `automatic|manual|none`. Manual adds `windows[]`, each with
`period: five_hour|day|week|month`, `unit: money|tokens`, positive decimal-string
`limit`, and `reset: {anchor, timezone}`. Money uses the existing ledger's USD
subcent precision; the UI shows currency explicitly and does not convert credits.
The anchor supplies local reset time, weekday/day-of-month, and fixed-window
phase. Named timezone is validated; month-end dates clamp to the last valid day.

Only each tier's first row stores `tier`. Every row stores its own model options
and join flag. The shared keep preference is independent of tier mode: add
`keep_model_while_running` to the existing dynamic-profile record and DTO, with
an additive database column defaulting true. This preserves the preference even
when the user removes every row, saves, and later adds candidates. It is a
continuity flag, not a profile selection mode. Use existing SQLite/PostgreSQL
settings migrations; omitted updates preserve the value. Empty lists cannot launch.

Expose `selection` beside candidate `policies` in
`DynamicAgentCandidateDTO`, with camelCase normalization into web types.
Controller codecs split/join the document; runtime policy decoders receive only
the policy portion. Keep selection validation in a small dedicated helper,
not in `routingpolicy.Evaluate`. Position sorting precedes tier derivation.
Reject first-row joins, invalid enums, nonpositive limits, nonfinite numbers,
invalid reset anchors, conflicting head-only metadata and duplicate candidates.
Missing selection means legacy defaults, not an invalid record.

## Compatibility and editing

Read empty rules, legacy action maps and existing version-1 documents. Preserve
transient, hard and unclassified policies during normalization. Store/reload,
duplicate, REST/WS projections and draft comparison must all round-trip selection.
Use existing optimistic dynamic-profile versioning and the atomic route replace.
Do not rewrite every route JSON document on startup. The only planned settings
schema change is the default-true continuity column on `dynamic_agent_profiles`.

An absent `selection` in an update preserves the existing row's configuration
by concrete profile ID; an explicit complete selection object changes it.
New rows use defaults. Old-client reorder requests with omitted selection must
apply the same join-boundary normalization server-side, or reject a conflict;
never silently erase joins or attach head settings to another tier.
Old-client behavior is read/write compatibility with the new server, not a claim
that an older server can preserve unknown fields when it rewrites JSON.
Downgrades remain a delivery risk to communicate.

## Group mutations

Derive tier blocks before filtering disabled rows, so a disabled head still owns
its tier policy. Stable identity is the concrete profile ID, never a badge number.

- Toggle join: joining merges into the upper block and uses its policy;
  unjoining splits the block and copies its policy to both new heads.
- Same-tier adjacent swap: rebuild that block's first-row ownership and join
  flags while preserving the block policy and row model options.
- Cross-tier adjacent swap: break join edges whose endpoints are no longer
  adjacent; force the moved row unjoined, and never create a new edge merely
  because two rows became neighbors. Preserve unchanged edges. Each resulting
  fragment inherits its originating block's policy.
- Remove: remaining members of the same block stay grouped and the next member
  inherits head policy. No merge with the next unrelated block. Add: singleton.
- After every mutation: renumber positions and derived badges; preserve the
  independent profile keep preference, including after the final row is removed.

Example: `[A =B] [C =D]`, moving B down across the boundary produces
`[A] [C] [B] [D]`; broken B-A and D-C joins do not bind new neighbors.
Moving A down inside `[A =B =C]` produces `[B =A =C]` with the same tier policy.

## Usage input and ranking

Add a small injected usage snapshot interface at the runtime composition
boundary. Fetch outside `Engine.mu`; use one observation time and one bounded
snapshot for a decision, then recheck generation and circuit eligibility during
the existing atomic claim. Do not add a background rerouting scheduler.
Reuse `UsageService` caching (five minutes, failed fetch cache fifteen seconds)
and coalescing. A failed refresh becomes unknown; preview may retain a visibly
stale last observation but selection does not treat it as fresh.

Extend normalized windows additively with numeric duration/start and, for
model-specific windows, applicability. Codex retains its numeric duration;
Claude maps known provider window kinds before display localization. Unknown
duration or ambiguous model scope is unavailable for that score, not parsed from
an English display label. Do not fabricate reset times for routing if the
provider only supplied an invalid/missing reset; display fallback is not evidence.

For a valid current window, `elapsed = clamp((now-start)/(reset-start), 0, 1)`
and `pace = usage_fraction / max(elapsed, 0.05)`. Do not cap usage at 100% before
computing pace. Select the largest pace over applicable available windows.
Expired windows require refreshed evidence; future starts and zero lengths are
invalid. Unknown-only candidates sort after known; ties use saved row order.

Free plus explicitly no window yields zero pace. Subscription/metered without
usable windows stays unknown. Circuit eligibility is checked before ranking,
including free candidates. The existing Kandev one-minute or trusted-reset
backoff remains; no external escalating-backoff policy is imported.

Cost order is free, subscription, metered, then unknown
legacy classification; row order breaks ties within a class. This reflects
configured marginal-cost classes and does not claim numerical metered-price
optimization. No numerical price or workload mix is configured for this rule.

For a nonzero reservation, leave raw pace unchanged; use
`observed_usage_fraction >= 1 - reserved_share` as an admission exclusion when
the share is positive and a usable limit exists. Zero adds no new quota gate.
Unknown usage cannot enforce a nonzero reserve and must be shown as unknown;
exclude the candidate until evidence returns. A positive
reserve with `none` is invalid. Values are 0 through 100 inclusive; 100 excludes
new selections. This rule does not seed an operational preset.

## Manual accounting

Use the existing task usage writer and immutable `task_usage_events`; do not
sum mutable session lifetime totals for a reset window. Query the half-open
interval `[window_start, min(now, reset))`, aggregate stored `tokens_total` or
`cost_subcents`, and retain price/completeness information. A 5-hour window uses
its anchor and fixed duration; day/week/month use calendar boundaries in the
configured timezone, including DST and leap/month-end handling.

The scope is the row's concrete execution profile and actual model
across its Kandev sessions, not all usage under the logical dynamic profile.
Resolve attribution by joining `task_usage_events.turn_id` to
`task_session_turns.id`, checking task/session ownership and the stored concrete
profile. For ordinary concrete sessions, a proven concrete event profile is also
usable. Do not use a dynamic event profile as a concrete profile or fall back to
the session's mutable current route. Missing/deleted turns or ambiguous legacy
rows remain unattributed. No new ledger column is planned; if integration proves
the existing turn contract insufficient, report that concrete gap before expanding
the schema. Keep token subset arithmetic and event deduplication with Costs.

A successful empty query is zero recorded usage, not proof that a subscription
has never been used. Unpriced money events, missing attribution or partial
measurements in the relevant input produce unknown pace with a visible recorded
lower bound. Deleted spend records and usage outside Kandev cannot be recovered
by this query; label the accounting as recorded usage and do not introduce a new
history-completeness tracking service. Manual totals are not provider-wide remaining
quota. This first
design does not invent a shared-account budget grouping control. DevPass monthly
values must use the owner's unit/reset; API-equivalent money may not equal
provider credit billing and must not be mislabeled as exact remaining credit.

## Decision flow and durable chain

Extend `dynamic.Candidate` with normalized tier/model metadata and
`dynamic.Profile` with the keep preference. Extract a pure ranking helper from
the growing engine. Consumers still pass one logical profile ID.

1. On a new session or permitted fresh selection, evaluate tiers in list order.
   Filter disabled/deleted/unlaunchable rows, current circuits and chain exclusions.
2. Rank only the first tier with eligible candidates using its rule. Unknown
   pace is still eligible after known pace unless explicit reservation blocks it.
3. Claim one generation and record the choice before downstream launch. Do not
   acquire half-open probe leases merely to score or preview candidates; only
   the selected candidate attempts the existing probe claim. Reconsider on claim
   contention without marking an unlaunched candidate as tried.
4. For failure, keep `routingpolicy.Evaluate` and effect-safe admission first.
   Wait/retry retains the candidate and schedule. Stop never invokes ranking.
5. Only skip/fallback uses same-tier-next or next-tier direction. Exhausted
   tiers advance forward; no wrap. The unclassified path reuses this selector
   only after its existing task-only evidence/threshold/workflow fences pass.

Add a bounded `selection_chain` subobject to `PolicyStateJSON`, carrying start
generation, logical profile identity, tried concrete IDs and last tier boundary
as candidate identities. Size is bounded by the candidate list/attempt chain.
Update it with the existing generation/status-fenced route decision transaction;
carry it through retry/reset, skip and restart. Do not clear it when policy
counters, unclassified streaks or launch status change.

Initial launch records its candidate; a permitted same-candidate retry ignores
the cross-candidate exclusion but does not reset it. Successful turn completion
closes the chain; `MarkActive` alone does not prove success. Explicit terminal
recovery may start a new chain when the user starts a new attempt, respecting
existing manual retry behavior. Profile edits preserve tried IDs for the active
chain and intersect eligibility with current rows, rather than clearing history.
If the old tier boundary cannot be reconciled after an edit, use existing manual
recovery instead of returning to earlier rows. All-ineligible state persists the
chain and reason across restart; no timer silently clears tried candidates.

## Continuity

Keep-model on binds the healthy selected route across turns, detach/resume and
usage refresh. New session and eligible failure are the normal reevaluation
points. Explicit profile disable/remove remains governed by safe-boundary
recovery. Cross-candidate continuation retains logical session and discards
provider-specific resume identity exactly as the existing conductor does.

When keep-model is off, compare before a new user turn at an idle safe
boundary; never interrupt an admitted turn or background work. Do not implement
a polling preemption loop.

## Preview and reasons

Propose `POST /api/v1/agent-profiles/:id/dynamic-preview` and
`POST /api/v1/agent-profiles/dynamic-preview` for an unsaved create draft. The body
uses the canonical draft candidate DTO and expected saved version where present.
The operation is computationally read-only. Validate scope and candidates as
for editing; never save, claim a generation/probe, open a circuit or launch an agent.
Draft previews work before the profile is saved. Existing settings lock permits
read-only preview but continues to block all mutation.

Return candidate ID, derived tier, mode, choice reason, eligibility reasons,
observation time, controlling window, usage percent, raw elapsed percent,
calculation elapsed floor, pace and provenance. Backend preview and execution
share the pure ranking function and input adapters. Frontend request identity
includes draft revision; stale responses cannot overwrite a newer draft.

Reuse `dynamic_route_attempts.reason`, not a new log table. Proposed bounded
codes are `tier_order`, `tier_pace`, `tier_cost`, with `_same_tier` or `_next_tier`
suffixes on fallback. Leave established manual/retry cause codes intact; retain
failure class/code in existing policy state. For legacy absent-selection profiles,
preserve old reason codes where consumers depend on them. Show detailed preview
values without embedding credentials, provider response bodies or prompts in reasons.

## Presentation

Entry remains Settings > Agents > Dynamic profile. Desktop shows tier containers,
head policy controls, and row identity with up / `=` / down plus model settings
on the right. Keep failure-specific policy controls separate and explain their
precedence. All fields participate in the existing single save/conflict flow.

Phone branches with `useResponsiveBreakpoint`. The primary surface is a focused
tier list, not stacked desktop policy grids. Row actions remain visible; a
labeled Model settings action opens a full-height detail surface for cost,
usage windows and retry policies. A short Tier settings action uses an inset
bottom Drawer. Shared draft state survives detail navigation and breakpoint
changes; detail Done returns to the list without prematurely persisting.

Inspected curated exemplar:
`apps/web/components/kanban/mobile-menu-sheet.tsx::ResponsiveMenuSurface`.
Reuse its inset Drawer, fixed header, `100dvh` bound, `min-h-0` scroll body and
bottom safe area, not its domain-specific menu logic. The existing
`mobile-dynamic-agent-profile-card.spec.ts` supplies route and seeded-profile
precedent; its legacy tooltip behavior is not the model for new touch help.

Each active surface has one vertical scroll owner. Save/back stays reachable
above safe areas/keyboard. Desktop ordinary controls retain 28px sizing; phone
and coarse pointer actions measure at least 44px on both icon dimensions.
Use shared settings sizing helpers and `@kandev/ui` primitives, focus return and
localized accessible names containing candidate identity. Preserve draft state
at 767/768px and narrow fine-pointer layouts; no hover-only controls or horizontal
document overflow. Empty, pending, invalid-reset, unavailable-usage and locked
states are specified in the plan previews.

## Authorization and compatibility risks

Reuse current settings access and the interim settings interlock. No settings
lock bypass, credential changes, new fingerprinting, external producer, profile
global mode or operational presets are introduced. Automatic usage must not
read unrelated runtime homes; unsupported binding stays unknown/manual.
The usage API, reset normalization and ledger attribution require targeted
fixtures before their accuracy is claimed. Existing hashes are not expanded.

## Related documents

- [Requirements](../requirements/dynamic-profile-tier-selection.md)
- [Plan and work orders](../../../plans/dynamic-profile-tier-selection/plan.md)
- [Dynamic routing ADR](../../../decisions/2026-08-13-dynamic-agent-profile-routing.md)
- [Error policy ADR](../../../decisions/2026-08-17-provider-error-classes-and-policies.md)
- [Conversation usage](../../costs/system-design/conversation-usage.md)
