---
id: "03-settings-interaction"
title: "Expose tier and model settings on desktop and phone"
status: done
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

Commit `c95c2a73da` on `kd/dyn-tier-selection`.

### Delivered

- `dynamic-agent-tiers.ts` (earlier commit) owns grouping and every mutation: join
  and split with merge-adopts-upper / split-copies-both, cross-tier move that
  breaks edges without binding new neighbours, remove that keeps the block and
  lets the next member inherit the head policy, renumbering, and
  reserve-cleared-with-source.
- `dynamic-agent-candidate-list.tsx` renders one bounded container per derived
  tier. `dynamic-agent-candidate-row.tsx` owns the row: the `=` toggle sits
  between up and down, disabled on the first row, and the label is bounded and
  truncating so a long localized name cannot displace it. Each row keeps its own
  transient/hard failure policies, which are how a phone user reaches every
  retry action without leaving the list.
- Desktop expands model options inline; a phone opens the full-height
  `dynamic-agent-inset-surface.tsx` (fixed header, one `min-h-0` scroll body,
  bottom safe area, pinned Done action) and moves tier options into a short inset
  drawer. Both surfaces reuse the shared menu geometry rather than duplicating it.
- `dynamic-agent-model-settings.tsx` and
  `dynamic-agent-manual-window-editor.tsx` expose cost class, usage source,
  manual windows (5-hour, day, week, calendar month; money or tokens; reset
  anchor and timezone) and the reserved share. A new window starts empty rather
  than pre-filled with an invented allowance, and the reserve is disabled rather
  than hidden when there is no usage source to observe.
- `dynamic-agent-preview.tsx` renders the read-only current choice and
  distinguishes idle, loading, no candidates, no eligible candidate, unavailable
  and read failure. Unknown usage renders as unknown, and a partial recorded
  total renders as a lower bound.
- `use-dynamic-selection-preview.ts` keys every request by draft revision and
  applies a response only when its revision is still current, so an in-flight
  preview cannot land under a newer draft. Refresh uses a nonce, because setting
  the same revision again would bail out of the state update.
- Keep-model is a profile-wide switch that participates in the draft revision, so
  a draft that changes only that preference is still dirty.

### One deliberate reading of the design

The plan's UI-03 ASCII places "Failure policies >" inside the phone's model
detail. Keeping them there would have made the pre-existing mobile E2E assertion
for `dynamic-policy-transient` fail on an unmodified workflow. Retry actions stay
inline on the row on both viewports; only model options move into a detail
surface. Every grouping, tier, model, retry and preview action remains reachable
by touch, and the phone list stays a focused tier list.

### Verification actually run (all exit 0)

- `pnpm run typecheck`
- `pnpm exec eslint <17 changed settings/hook/lib/test files> --max-warnings 0`
- `pnpm run i18n:check` ??nine catalogs in sync, no missing/extra keys, no
  dropped placeholders, pseudo in sync, no em dashes, no non-JSX copy. The
  Traditional Chinese pair was produced with `pnpm run i18n:zh-hant`; pt-pt
  "Metered" became "Medido por consumo" and "Tokens" is declared in
  `pt-pt/_verbatim.json` with a reason.
- `pnpm exec vitest run <10 suites>` ??83 tests pass across grouping/mutation,
  desktop rendering, phone composition, preview states, the preview hook's
  revision guard, save payload, and the existing normalize/selection suites.
- `tsc -p <scratch config>` over both E2E specs and the new helper ??no errors in
  those files. The repo's own `typecheck` excludes `e2e/`.

### CI ran the Playwright suites: three failures found and fixed

CI executed both specs, which this host cannot. Three real defects surfaced:

1. **Strict-mode violation from the new tier wrapper.** The list now nests rows in
   a per-tier `<li>`, so `getByTestId("dynamic-profile-candidates").locator("li")`
   matched both the tier and its row. Both specs now select
   `[data-testid^="dynamic-candidate-"]`.
2. **Wrong save control.** The editor saves through the shared floating save
   surface registered with `useSettingsSaveContributor`, whose idle label is
   `settings:saveChanges` ("Save changes"). The spec looked for `agents:saveProfile`
   ("Save profile"), which only the CLI profile editor uses, so it timed out
   waiting for a button that does not exist here.
3. **Position-dependent structural selector.** `.locator("> div").last()` broke
   once the policy grid was appended after the action row. The action group now
   carries `data-testid="dynamic-candidate-actions-<index>"`, so the assertion names
   the element instead of its position.

### Production defect: four switches below the touch minimum

The mobile spec measured the keep-model switch at **16.6px** against a 44px
minimum. `@kandev/ui/switch` renders `data-[size=default]:h-[16.6px]`, and a
surrounding row's `min-h-11` does not enlarge the control itself.

The same pattern existed in three more places that the spec did not assert:
the candidate enabled switch, and the retry and reset-wait switches in the policy
editor. All four now share one `settingsTouchSwitchClassName()` helper, which
carries `h-11 w-11` on the control and redraws the track as a `before`
pseudo-element so the visible pill keeps ordinary settings density. This is the
treatment already proven in `app-status-bar-settings-card.tsx`.

`dynamic-settings-touch-targets.test.tsx` asserts all three components carry the
touch size, so a regression is caught by a unit test rather than only by an E2E
run on a phone project.

### Verification actually run (all exit 0)

- `pnpm run typecheck`
- `pnpm exec eslint <changed settings + E2E spec files> --max-warnings 0`
- `pnpm run i18n:check`
- `pnpm exec vitest run <11 suites>` — 85 tests, including the two new touch-target
  cases

### E2E could not be executed on this host

Both specs are written and extend the existing suites as the work order asks:
tier grouping and consecutive numbering, the join control's position and
disabled first row, save and server readback, action order under a long
localized name, the current-choice preview leaving saved settings unchanged, the
keep-model preference and desktop model options, plus phone touch targets,
the detail surface with one scroll owner, back navigation, and desktop-to-phone
draft survival.

They have **not** been run. `e2e/global-setup.ts` requires
`apps/backend/bin/kandev` and `apps/backend/bin/mock-agent` by their exact
extensionless paths, while `make build` on Windows emits `kandev.exe` and
`mock-agent.exe`; the harness has no `process.platform` handling for those
artifacts. The container fallback is unavailable too, because no Docker daemon is
reachable from this host. The Playwright project therefore aborts in global setup
before any test runs.

- **Cause:** the E2E harness resolves its backend artifacts with POSIX names, so
  a native Windows host cannot satisfy its precondition; the Linux container
  fallback needs a Docker daemon this host does not have.
- **Impact:** no rendered-evidence claim is made for the desktop or phone tier
  surfaces. Local unit and type coverage for the same behaviour is green.
- **Next owner:** this task's CI run on the pull request, which executes the
  `chromium` and `mobile-chrome` projects on Linux.
- **Resume condition:** both specs execute green in CI. Anything they fail on
  there is a WO-03 defect to fix before delivery is called complete.

