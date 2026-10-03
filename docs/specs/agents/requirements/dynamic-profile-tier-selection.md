---
status: draft
system: agents
created: 2026-10-02
owners:
  - kandev
---

# Dynamic Profile Tier Selection Requirements

## Overview

Users group adjacent concrete candidates in a dynamic profile and choose how
each group selects and advances. Agents owns this profile contract across task,
Office and utility consumers. The Costs system continues to own usage facts.
Desktop and phone expose the same settings and explanations.

This is a proposed extension of [dynamic routing](dynamic-agent-routing.md),
not a replacement router. A tier is a contiguous group within one profile,
not an Office capability tier or a semantic interpretation of a profile name.
No explicit numeric tier identity is stored. The existing profile identity,
candidate eligibility, failure classification and safe continuation contracts
remain applicable. Group selection replaces flat ordering only where configured;
the no-revisit rule applies to automatic transition chains in both layouts.

## Terminology

- **Join:** a row's relationship to the immediately preceding row.
- **Tier:** one maximal contiguous run connected by joins, numbered from one.
- **Pace:** usage fraction divided by elapsed fraction in the applicable window.
- **Transition chain:** one admitted execution attempt and its automatic retries
  and candidate changes, ending in success or explicit terminal recovery.

## Requirements

### REQ-AGENTS-TIER-SELECTION-001: Adjacent grouping

- **AC-AGENTS-TIER-SELECTION-001.1:** Every candidate row shall expose an `=`
  toggle between its up and down controls. It shall join or unjoin the row with
  the row above; the first row's toggle shall be disabled and unjoined.
- **AC-AGENTS-TIER-SELECTION-001.2:** Contiguous joined rows shall display one
  bounded tier with consecutive numbering. Moving a row across a tier boundary
  shall clear affected joins instead of implicitly joining unrelated neighbors.
  Reordering within one tier shall retain that tier and its policy.
- **AC-AGENTS-TIER-SELECTION-001.3:** Each tier shall offer ordered, lowest-pace
  and lowest-cost selection, and same-tier-next or next-tier-on-failure
  direction. Tiers shall always be traversed in list order. There shall be no
  profile-level selection mode. Joining all rows permits whole-list comparison.
- **AC-AGENTS-TIER-SELECTION-001.4:** Removing, splitting or merging rows shall
  retain model settings with their concrete candidate and show the resulting
  policy before save. A merge uses the upper tier's policy; split tiers inherit
  the previous policy. Empty groups shall not survive and numbers shall close up.

### REQ-AGENTS-TIER-SELECTION-002: Compatible configuration

- **AC-AGENTS-TIER-SELECTION-002.1:** Existing profiles without joins or selection
  settings shall retain ordered candidate behavior, existing failure policies,
  and unchanged concrete launch configuration. New selection defaults shall be
  ordered, same-tier-next, reservation zero, and keep-model enabled.
- **AC-AGENTS-TIER-SELECTION-002.2:** Save, reload, duplication, draft reconciliation
  and unrelated edits shall preserve joins, first-row tier settings, model
  settings and the profile keep-model preference. A version conflict or invalid
  setting shall leave saved settings unchanged and preserve the local draft.
- **AC-AGENTS-TIER-SELECTION-002.3:** Existing settings authorization and locking
  shall apply. Preview shall not save settings, start work or alter route health.
  A legacy client omitting the new fields shall not erase an existing selection
  configuration on an otherwise valid update.

### REQ-AGENTS-TIER-SELECTION-003: Usage pace and model inputs

- **AC-AGENTS-TIER-SELECTION-003.1:** For each usable window, pace shall equal
  usage fraction divided by elapsed window fraction, with elapsed clamped to a
  minimum of 0.05. Window start is reset time minus window length. Multiple
  windows shall use their largest pace; 25% usage at 40% elapsed yields 0.625.
- **AC-AGENTS-TIER-SELECTION-003.2:** Known pace shall sort before unknown pace;
  equal values shall retain row order. A free candidate explicitly configured
  without a window shall have pace zero but shall remain subject to existing
  circuit/backoff restrictions. Missing, expired, invalid or unavailable usage
  shall not be represented as zero usage or unlimited capacity.
- **AC-AGENTS-TIER-SELECTION-003.3:** Each model row shall offer free,
  subscription or metered cost classification, automatic usage, manual limits,
  or no usage source. Automatic usage shall match the candidate's actual
  supported account binding; unavailable bindings shall be reported rather than
  replaced by another account's usage. Usage belongs to the concrete profile's
  account ([provider account usage](../../costs/requirements/provider-account-usage.md)),
  so a row without manual limits ranks on that account's reading whenever one
  exists; no usage source only decides the free/unknown fallback without one.
- **AC-AGENTS-TIER-SELECTION-003.4:** Manual limits shall support 5-hour, day,
  week and calendar-month windows, a reset basis, and a positive money or token
  allowance. Kandev-recorded session usage in that window shall determine the
  fraction. Monthly providers such as DevPass shall use calendar-month reset
  boundaries rather than a fixed 30-day duration. Partial accounting and unknown
  prices shall remain visible as incomplete data.
- **AC-AGENTS-TIER-SELECTION-003.5:** User-reserved share shall default to 0% and
  be editable per row. It shall not silently reserve capacity by default. A
  configured share shall be shown separately from observed usage and elapsed
  percentages; it shall not modify the definition of pace.
- **AC-AGENTS-TIER-SELECTION-003.6:** Lowest-cost selection shall operate only
  within its tier and use the configured cost classification, with deterministic
  row-order ties. It shall not invent provider prices, perform currency
  conversion, or treat unavailable pricing as a confirmed zero charge.

### REQ-AGENTS-TIER-SELECTION-004: Safe advancement and continuity

- **AC-AGENTS-TIER-SELECTION-004.1:** Existing failure-specific reset waits and
  same-candidate retries shall run before a tier's fallback direction. Stop,
  effect-safety, cancellation, stale-generation and recovery barriers shall
  continue to veto automatic changes. Same-candidate retries are not new
  cross-candidate selections.
- **AC-AGENTS-TIER-SELECTION-004.2:** On permitted fallback, same-tier-next shall
  try an eligible untried candidate using the tier's selection rule, then move
  to subsequent tiers when exhausted. Next-tier-on-failure shall skip all
  remaining peers. Neither direction shall return to an earlier tier or a
  previously tried candidate in that transition chain, including after restart.
- **AC-AGENTS-TIER-SELECTION-004.3:** Keep-model shall default on. While work
  runs, usage refresh or newly available capacity shall not change the selected
  model. Normal capacity comparison shall occur at new-session start or eligible
  failure. Healthy resume and further turns in that session shall retain the
  selection. Existing explicit disable/remove and manual recovery semantics
  shall remain intact.
- **AC-AGENTS-TIER-SELECTION-004.4:** All-ineligible or exhausted chains shall
  use existing waiting/manual recovery with a reason, without starting another
  session, spinning or recycling tried candidates. Successful selection shall
  preserve logical session identity and concrete turn attribution.
- **AC-AGENTS-TIER-SELECTION-004.5:** Profile changes, duplicate failures and
  concurrent transitions shall not reset the chain's exclusions or launch two
  successors. The existing narrower scope of unclassified fallback shall not
  expand to Office or utility calls.

### REQ-AGENTS-TIER-SELECTION-005: Explanation and settings parity

- **AC-AGENTS-TIER-SELECTION-005.1:** A current-choice preview shall name the
  selected candidate, tier, selection reason, controlling window, usage percent,
  elapsed percent and freshness/basis. It shall distinguish unknown usage,
  loading, read failure, no candidates and no eligible candidates. Preview is a
  prediction for a new selection, not a promise to switch an active session.
- **AC-AGENTS-TIER-SELECTION-005.2:** Persisted route attempts shall identify
  ordered, pace or cost selection and relevant fallback direction without
  including credentials or raw provider responses. Preview and actual selection
  shall agree when profile, clock, health and usage inputs match.
- **AC-AGENTS-TIER-SELECTION-005.3:** Desktop and native phone settings flows
  shall support every grouping, tier, model, retry and preview action. Phone
  composition shall use a focused tier list and explicit detail navigation,
  with at least 44px touch targets, one scroll owner per surface, safe-area-aware
  actions, no document horizontal overflow and keyboard/focus accessibility.
- **AC-AGENTS-TIER-SELECTION-005.4:** Draft settings shall survive desktop/phone
  viewport changes, detail back navigation and failed saves. All new copy shall
  use localization, including the fork's Korean catalog and existing supported
  catalogs. Preview failures shall not clear drafts or saved configuration.

## Exclusions

No external hint file or producer, global selection mode, operational preset,
provider credential changes, proactive interruption, new provider integration,
new budget service, separate Office router, or automatic runtime replacement.
Actual tier values and manual subscription allowances belong to the user.
Three-model review and delivery sequencing belong to the implementation plan.

## Design and delivery

- [System design](../system-design/dynamic-profile-tier-selection.md)
- [Implementation plan](../../../plans/dynamic-profile-tier-selection/plan.md)
