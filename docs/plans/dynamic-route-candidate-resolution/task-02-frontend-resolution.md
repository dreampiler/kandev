---
id: "02-frontend-resolution"
title: "Frontend dynamic-reference resolution surfaces"
status: done
depends_on: ["01-backend-resolution"]
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001
system_design: "../../specs/agents/system-design/dynamic-route-candidate-resolution.md"
---

# Task 02: Frontend resolution surfaces

## Outcome

The administrator sees a resolution confirmation instead of a dead-end save
failure from the profile editor, the Agents list-row toggle, and the delete flow,
on desktop and phone, and can disable or remove the candidate reference before
the profile is disabled or deleted.

## In scope

- Add the `DynamicProfileReference` wire type and parse `dynamic_profiles` in the
  conflict error path (`DeleteProfileResult` and the update error path).
- Add the resolution dialog offering the documented choices plus Cancel, reusing
  the existing alert-dialog and mobile confirmation primitives. On the delete
  entry it offers disabling instead of deleting (the copy states the profile is
  not deleted) and removing then deleting.
- Add `resolveDynamicReferencesAction` and wire it into `useProfileSave`,
  `useProfileEnabledToggle`, and `useProfileDelete`.
- Add all new copy through the `t()`/`Trans` catalog contract in every catalog,
  including `ko`.

## Exclusions

- No backend change; Task 01 owns the contract.
- No new settings screen, permission model, or bulk dynamic editor.
- No change to the routing-tier/utility/session/watcher confirmations.
- No new E2E/integration suite or collection path.

## Applicable IDs

- `REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001`
- `AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.1`, `.2`, `.8`, `.9`

## Design references

- `docs/specs/agents/system-design/dynamic-route-candidate-resolution.md`
  (Data and contracts, Frontend resolution surfaces).

## ASCII UI preview

UI-01 (excerpt; full preview in the plan's "ASCII UI preview"): the dialog lists
each referencing dynamic profile with its tier/position. On the disable entry it
offers "disable candidate in every dynamic profile" and "remove candidate from
every dynamic profile" plus Cancel; on the delete entry it offers "disable the
candidate and disable this profile instead of deleting it" and "remove the
candidate and delete this profile" plus Cancel. The least destructive positive
choice is the Enter default; the phone container keeps one scroll owner and 44px
targets. Maps to AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.1, .2, .9.

## Implementation acceptance conditions

1. A `dynamic_profiles` 409 from any of the three surfaces opens the resolution
   dialog naming each profile and its tier/position, with the documented choices
   and Cancel, including the delete entry's disable-instead and remove-then-delete
   choices.
2. Confirming calls the resolution action with the echoed versions and publishes
   the returned profile (or removes it on delete); Cancel changes nothing.
3. Desktop and phone present the same choices; copy resolves through the catalog
   contract and `i18n:check`/`i18n:ratchet` pass.

## Verification commands

- From `apps/web`: `pnpm test -- <changed vitest files>`, then
  `pnpm run typecheck`, `pnpm run lint`, `pnpm run i18n:check`,
  `pnpm run i18n:ratchet`.

## Likely files

- `apps/web/app/actions/agents.ts`
- `apps/web/lib/types/agent-profile-errors.ts`
- `apps/web/components/settings/agent-profile-delete-dialog.tsx`
- `apps/web/components/settings/agent-profile-page-state.ts`
- `apps/web/hooks/domains/settings/use-profile-enabled-toggle.ts`
- `apps/web/src/locales/<locale>/agents.json`

## Dependencies

Task 01.

## Results

Done. `dynamic_profiles` is parsed on the update and delete error paths, the
shared `AgentProfileDynamicReferenceDialog` plus its `useDynamicReferenceDialog`
hook render the documented choices and Cancel, and the resolution action is wired
into `useProfileSave`, `useProfileEnabledToggle`, and `useProfileDelete` (editor
save, list-row toggle, delete flow). Copy is added in every catalog including
`ko`. `tsc`, `eslint`, the changed `vitest` files (34 passing),
`i18n:check`, and `i18n:ratchet` pass.
