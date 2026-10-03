---
created: 2026-10-03
status: done
requirements:
  - REQ-COSTS-PROVIDER-USAGE-001
  - REQ-COSTS-PROVIDER-USAGE-002
  - REQ-COSTS-PROVIDER-USAGE-003
  - REQ-COSTS-PROVIDER-USAGE-004
  - REQ-COSTS-PROVIDER-USAGE-005
  - REQ-COSTS-PROVIDER-USAGE-006
  - REQ-AGENTS-TIER-SELECTION-001
  - REQ-AGENTS-TIER-SELECTION-003
system_design:
  - ../../specs/costs/system-design/provider-account-usage.md
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
legacy_specs: []
---

# Implementation Plan: Provider Account Usage

## Overview

Every dynamic candidate reported unknown usage. The usage adapter used
`agent_profiles.agent_id`, an agent row ID, as the registry agent type, so no
profile ever matched a usage client. This plan fixes that, adds the OpenCode
provider accounts, scopes class-specific quotas, adds account-scoped manual
windows and lists usage per profile. The
[requirements](../../specs/costs/requirements/provider-account-usage.md) and
[system design](../../specs/costs/system-design/provider-account-usage.md)
define the behavior.

Evidence before the change: an installed instance's dynamic preview reported
`usage_known=false` for all candidates, including rows set to automatic, and
the agents list returned `billing_type=subscription` for Codex profiles whose
usage endpoint answered 200 with a weekly window.

## Work orders

- [x] [Task 01: Backend usage sources and list API](task-01-backend-usage-sources.md)
- [x] [Task 02: Usage on the agents settings list](task-02-agents-list-usage.md) (depends on Task 01)
- [x] [Task 03: Selection modes and internal usage](task-03-selection-modes-and-internal-usage.md) (depends on Task 01)

## ASCII UI preview

`UI-01: Settings > Agents profile row` (Task 02). The usage line sits under the
profile name and model; illustrative values.

```text
+--------------------------------------------------------------------+
| 6 Luna                     gpt-6-luna                  [Edit] [...] |
|   7-day 23%  resets in 5d 6h   (provider)                           |
+--------------------------------------------------------------------+
| OCG-GLM                    opencode-go/glm-5.3         [Edit] [...] |
|   5-hour 0%  7-day 100% limit reached, resets in 1d 9h              |
|   monthly 50%, resets in 26d                                        |
+--------------------------------------------------------------------+
| OCZ-BigPickle Free         opencode/big-pickle         [Edit] [...] |
|   No usage API. Recorded today: 41 turns, 3.2M tokens (5 profiles)  |
+--------------------------------------------------------------------+
| Sonnet 5                   sonnet                      [Edit] [...] |
|   Usage unavailable: credential missing                             |
+--------------------------------------------------------------------+
```

`UI-02: Dynamic profile preview` keeps the selected candidate, its tier, reason
and controlling window; the per-candidate usage list moves behind a disclosure
and links to Settings > Agents. Phone uses the same stacked rows.

## Risks

- Claude long-lived tokens may lack the usage endpoint's scope; the state then
  reads unauthorized instead of borrowing another account.
- The LLM Gateway month has no published reset, so its pace is known only when
  exhausted.
- Free Go models are assumed outside the Go plan windows.

## Verification strategy

Unit tests with recorded provider response shapes; one disposable live call of
the adapter against the real providers (not committed); installed-instance
readback after replacement.
