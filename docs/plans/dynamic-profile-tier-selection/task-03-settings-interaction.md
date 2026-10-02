---
id: "03-settings-interaction"
title: "Expose tier and model settings on desktop and phone"
status: pending
wave: 3
depends_on:
  - "02-tier-routing"
plan: "plan.md"
requirements:
  - REQ-AGENTS-TIER-SELECTION-001
  - REQ-AGENTS-TIER-SELECTION-002
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-AGENTS-TIER-SELECTION-004
  - REQ-AGENTS-TIER-SELECTION-005
acceptance_criteria:
  - AC-AGENTS-TIER-SELECTION-001.1
  - AC-AGENTS-TIER-SELECTION-001.2
  - AC-AGENTS-TIER-SELECTION-001.3
  - AC-AGENTS-TIER-SELECTION-001.4
  - AC-AGENTS-TIER-SELECTION-002.2
  - AC-AGENTS-TIER-SELECTION-002.3
  - AC-AGENTS-TIER-SELECTION-003.3
  - AC-AGENTS-TIER-SELECTION-003.4
  - AC-AGENTS-TIER-SELECTION-003.5
  - AC-AGENTS-TIER-SELECTION-004.3
  - AC-AGENTS-TIER-SELECTION-005.1
  - AC-AGENTS-TIER-SELECTION-005.3
  - AC-AGENTS-TIER-SELECTION-005.4
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
---

# Task 03: Desktop and Native Phone Settings

## Summary

Implement the reviewed tier list, model settings and current-choice preview in
the existing dynamic-profile editor. Share draft/validation/save logic while
giving phone users focused detail surfaces and touch controls.

## In scope

Join/up/down mutations, group numbering/head policy ownership, cost/source/manual
windows/reserve, keep preference, preview states, lock/conflict behavior and all
supported translations. Reuse the existing save owner and API normalization.

## Out of scope

New runtime selectors, operational preset application, whole-settings redesign,
provider credential forms and a separate mobile business-logic implementation.

## Acceptance

1. All row/tier mutations preserve canonical ownership and per-model settings;
   save/reload/duplicate and breakpoint changes preserve the draft accurately.
2. Desktop and phone expose every option, preview state and existing failure
   policy, with the same authorization, localized copy and optimistic conflict flow.
3. Rendered phone controls meet 44px targets and safe-area/scroll/containment
   requirements, while ordinary desktop controls retain 28px sizing.

## ASCII UI preview

Excerpt of [UI-01/02/03 in the full plan](plan.md#ascii-ui-preview), covering
AC-001.1-4 and AC-005.1/3/4 (full IDs in frontmatter):

```text
UI-01 desktop
[Tier 1: Pace v | Same tier next v]
A [up] [= disabled] [down] [Model settings]
B [up] [= on]       [down] [Model settings]
Choose now: B | 25% used / 40% elapsed

UI-02 phone                   UI-03 model detail
[Back] Dynamic profile        [Back] B
[Tier 1] [Tier settings]       Cost [Subscription v]
B                             Usage [Manual v]
[up] [= on] [down]             Period [Month v]
[Model settings >]            Limit/unit/reset/timezone
[Add candidate]               Reserved share [0] %
[Cancel] [Save]               [Failure policies >] [Done]
```

Tier options use a short inset Drawer; long model detail has a full-height
surface with fixed header/action and one internal scroll body. The main list
is the phone entry, not a squeezed desktop grid. UI-04 states from the plan are
also required. Layout spacing/sample values are illustrative.

## Verification

From `apps/web/` after the plan's dependency install:

```powershell
pnpm run typecheck
pnpm exec vitest run components/settings/dynamic-agent-profile-editor-state.test.ts components/settings/dynamic-agents-card.test.tsx components/settings/dynamic-agent-policy-editor.test.tsx lib/api/domains/agent-profile-normalize.test.ts
pnpm run i18n:check
pnpm e2e:run --host --shards 1 --project chromium tests/settings/dynamic-agent-profile-card.spec.ts
pnpm e2e:run --host --shards 1 --project mobile-chrome tests/settings/mobile-dynamic-agent-profile-card.spec.ts
```

Run the plan's explicit ESLint command including new extracted components.
Use Git Bash for the shell runner on Windows, isolated test HOME/ports and mocks.
Extend existing E2E suites with grouping/reorder/policy/save/readback, model detail,
reset validation, preview unknown/loading/error, settings lock, conflict and
desktop-phone-desktop draft preservation. Measure targets/containment at Pixel 5,
767px fine pointer, 768px and a coarse tablet; inspect a rendered phone capture.

## Files likely touched

- `apps/web/components/settings/dynamic-agent-candidate-list.tsx`
- `apps/web/components/settings/dynamic-agent-profile-editor.tsx`
- `apps/web/components/settings/dynamic-agent-profile-editor-draft.ts`
- `apps/web/components/settings/dynamic-agent-profile-editor-state.ts`
- Proposed `dynamic-agent-tier-settings.tsx`, `dynamic-agent-model-settings.tsx`
  and a preview client/hook beside existing settings code
- `apps/web/lib/api/domains/agent-profile-normalize.ts`
- Existing dynamic profile settings test suites named above
- `apps/web/src/locales/*/agents.json` including `ko`, English and pseudo workflow

## Dependencies and risks

Requires Tasks 01/02 and settled optional toggle semantics. Inspect
`mobile-menu-sheet.tsx::ResponsiveMenuSurface` and shared settings sizing helpers.
Do not inherit unqualified 44px desktop sizes or rely on a tapped tooltip for
new touch help. Do not let stale preview responses overwrite the current draft.

## Results

Pending. ASCII previews are design artifacts, not rendered evidence.
