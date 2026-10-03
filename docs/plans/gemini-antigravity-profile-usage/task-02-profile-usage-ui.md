---
id: "02-profile-usage-ui"
title: "Show Antigravity quota in global profile settings"
status: pending
wave: 2
depends_on:
  - "01-profile-usage-read"
plan: "plan.md"
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-002
acceptance_criteria:
  - AC-COSTS-SUBSCRIPTION-USAGE-002.1
  - AC-COSTS-SUBSCRIPTION-USAGE-002.3
  - AC-COSTS-SUBSCRIPTION-USAGE-002.4
  - AC-COSTS-SUBSCRIPTION-USAGE-002.5
system_design:
  - ../../specs/costs/system-design/antigravity-usage.md
---

# Task 02: Show usage in global profile settings

## Summary

Add a read-only quota card to the saved global Antigravity profile page. Show every active Gemini window's label, used percentage, and local reset date/time as visible text on desktop and phone.

## Scope

- Fetch Task 01's endpoint using the saved profile ID, independent of the editable draft and save state.
- Localize the card, loading, unavailable, and retry states in all required locales.
- Preserve existing Office usage display and agent profile editing behavior. Do not create or bind an Office agent.

## Acceptance

1. Every returned window shows its label, percent, and visible reset text without hover; empty and failed reads show unavailable plus retry, never 0%.
2. The phone card follows the existing settings navigation and fits the page's single vertical scroll without horizontal overflow. Retry is keyboard accessible and has at least a 44px touch target.
3. Refetching or editing the profile cannot copy usage into the save draft or overwrite unsaved changes.

## ASCII UI preview

UI-01 and UI-02 use the same card content, with phone stacking the label and percent. UI-03 is the error state. See the [plan preview](plan.md#ascii-ui-preview); placement and persistent reset text are structural, while spacing and example values are illustrative.

```text
Desktop: Gemini usage | 7-day window     100% used | Resets <local date/time>
Phone:   Gemini usage
         7-day window
         100% used
         Resets <local date/time>
Error:   Gemini usage | Usage unavailable. [Retry]
```

## Verification

After implementation, from `apps/web`:

```text
pnpm run i18n:check
pnpm run typecheck
pnpm e2e:run --project chromium tests/settings/agent-profile-layout.spec.ts
pnpm e2e:run --project mobile-chrome tests/settings/mobile-agent-profile-layout.spec.ts
```

Reuse the existing `agent-profile-layout.spec.ts` and `mobile-agent-profile-layout.spec.ts` cases with the guarded E2E runner after its required build. For the changed card, run a disposable desktop and phone browser flow from ignored `temp/gemini-antigravity-profile-usage/`, outside Playwright's automatic collection. Stub the quota read only, follow the real settings navigation, check visible window/percent/reset text and unavailable/retry, and confirm no Office agent is created. On the phone, check the retry hit area, single page scroll, and no horizontal overflow. Record exact commands, viewport, and results in `plan.md`, then remove the disposable check. Do not add permanent test files without a separately approved change.

## Likely files and dependencies

- `apps/web/components/settings/agent-profile-page.tsx` and a small usage card/hook near it.
- `apps/web/lib/api/domains/` settings API read and `apps/web/src/locales/{en,pt-pt,zh-cn,zh-hk,zh-tw,ja}/agents.json` (confirm catalog paths before editing).
- Existing profile layout cases and disposable browser checks; Task 01's response contract must be available.

## Risks

The Office `UtilizationBars` reset time is tooltip-only. Do not reuse it unchanged for the required always-visible reset value. Keep the read-only card outside any disabled edit fieldset so source failures and retry stay usable.

## Results

Pending explicit implementation request.
