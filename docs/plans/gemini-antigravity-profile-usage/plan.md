---
created: 2026-10-01
status: draft
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-002
system_design:
  - ../../specs/costs/system-design/antigravity-usage.md
legacy_specs: []
---

# Implementation Plan: Global Antigravity Profile Usage

## Overview

Make Gemini usage readable from the saved global Antigravity profile in agent settings. The completed [provider plan](../gemini-antigravity-usage/plan.md) and [fork PR #23](https://github.com/dreampiler/kandev/pull/23) already provide the `agy` source, `Gemini Models` selection, window conversion, and cache. Deliver a profile-scoped read first, then the settings card and focused browser proof. Do not duplicate or replace the source.

## Scope

### In scope

- Read-only usage for an existing global `antigravity-acp` profile, independent of Office agent instances.
- Desktop and phone profile card with visible window, used percent, reset date/time, loading, unavailable, and retry states.
- Localized UI copy in the six required languages and focused desktop/mobile checks.

### Out of scope

- Office agent creation or rebinding; operating configuration changes; provider credentials or quota parser changes.
- Production implementation, permanent tests, builds, or deployment during this design turn.
- Other provider usage surfaces, scheduling, billing, and real-time polling.

## Technical approach

1. Add `GET /api/v1/agent-profiles/:id/utilization` in `apps/backend/internal/agent/settings/handlers/`, wired to the existing `backendapp/usage_adapter.go`. Resolve the saved profile through the settings repository, require `antigravity-acp`, and return the existing `ProviderUsage` envelope. Keep 404, null for unsupported profiles, and generic provider failure distinct. Reuse the existing five-minute cache and read access policy. Avoid a second `agy` caller.
2. Add a typed settings API read and a read-only quota card to `apps/web/components/settings/agent-profile-page.tsx`, keyed to the saved profile. The card must not participate in the editor draft or save state. Render `reset_at` as visible localized text; never depend on `UtilizationBars`' hover tooltip for the required value. Use existing profile page and settings card layout.

Compatibility: `antigravity-acp` via local `agy` yields the PR #23 `google`/windows shape. Other agent types return null. A missing CLI, invalid response, or no active Gemini bucket yields unavailable; no substitute model group or 0% placeholder. The Office projection continues using the same source and response type.

## ASCII UI preview

UI-01: Saved Antigravity profile, desktop, populated. Structural requirements: the quota card precedes editable settings; each active window has persistent reset text. Values below are illustrative.

```text
Antigravity / Default profile                 [Duplicate] [Enabled]
--------------------------------------------------------------
Gemini usage
  7-day window                  100% used
  [############################]
  Resets Oct 2, 2026, 3:47 AM (local time)
--------------------------------------------------------------
Profile settings ...
```

UI-02: Same saved profile, phone, populated. Existing settings page owns vertical scrolling; no nested scroll or drawer. The phone entry is Settings > Agents > Antigravity > saved profile.

```text
<  Antigravity / Default profile
   [Duplicate]  [Enabled]
Gemini usage
  7-day window
  100% used
  [####################]
  Resets Oct 2, 2026, 3:47 AM (local time)
Profile settings ...
```

UI-03: Source unavailable, either viewport. Keep the card in place, without a numeric value; retry is a visible button with a phone touch target of at least 44px.

```text
Gemini usage
  Usage unavailable.             [Retry]
```

## Tests

| Criteria | Focused evidence during implementation |
| --- | --- |
| AC-COSTS-SUBSCRIPTION-USAGE-002.2, .4 | Reuse existing profile handler tests, then make a disposable HTTP probe against a saved Antigravity profile with no Office agent; inspect missing and unsupported IDs. |
| AC-COSTS-SUBSCRIPTION-USAGE-002.3 | In that disposable probe, confirm the failure response contains no raw command details; inspect unavailable/retry in the rendered page. |
| AC-COSTS-SUBSCRIPTION-USAGE-002.1, .5 | Reuse existing agent profile layout browser cases, then check the populated card and retry through a disposable desktop/phone browser flow. |

## E2E tests

Reuse `apps/web/e2e/tests/settings/agent-profile-layout.spec.ts` in `chromium` and `apps/web/e2e/tests/settings/mobile-agent-profile-layout.spec.ts` in `mobile-chrome` for existing navigation/layout coverage. For the new quota behavior, run a disposable browser flow from the repository's ignored `temp/gemini-antigravity-profile-usage/` location, outside Playwright's automatic test collection. Stub only the profile quota read with deterministic `ProviderUsage`, navigate the real settings route on desktop and phone, and inspect visible window, percent, reset text, unavailable/retry, and absence of Office agent creation. The phone flow also checks one page scroll and no horizontal overflow. Record observed results and remove disposable files afterward. Keep source parsing evidence in PR #23 rather than retesting `agy` in the browser.

## Work orders

- [ ] [Task 01: Read usage by global profile](task-01-profile-usage-read.md)
- [ ] [Task 02: Show usage in global profile settings](task-02-profile-usage-ui.md) (depends on Task 01)

## Verification results

Design package only. PR #23 source and the current Office/settings paths were read; implementation checks have not run. The [provider plan](../gemini-antigravity-usage/plan.md) records its completed source verification. Implementation may start only after a later direct implementation request; then execute the two work orders in order and record their exact results here. Reuse existing tests; put any new checks in ignored `temp/gemini-antigravity-profile-usage/` outside automatic collection, and remove them after recording the result. Promoting a check into a permanent test requires a separately approved change.

## Risks

- The backend host may not have `agy` on PATH or may be signed into a different account than the user expects. Surface unavailable without exposing account data; do not change credentials as part of this work.
- `reset_at` is a UTC instant and the visible local date can cross a day boundary. Browser checks must assert the formatted value in a fixed test timezone.
