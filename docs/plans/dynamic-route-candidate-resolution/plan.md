---
created: 2026-10-13
status: draft
requirements:
  - REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001
system_design:
  - ../../specs/agents/system-design/dynamic-route-candidate-resolution.md
legacy_specs: []
---

# Implementation Plan: Resolve dynamic route references on profile disable/delete

## Overview

When a concrete agent profile is a candidate of one or more dynamic profiles,
the existing profile-in-use check already refuses to disable or delete it and
reports the referencing dynamic profiles. The settings UI, however, only shows a
generic save failure, so the administrator is stuck. This package adds a
confirmation that lets the administrator disable the candidate in place or remove
it from every referencing dynamic profile, applied atomically, before the
profile is disabled or deleted.

Agents owns the contract. The package extends
[REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001](../../specs/agents/requirements/dynamic-route-candidate-resolution.md)
and its
[system design](../../specs/agents/system-design/dynamic-route-candidate-resolution.md).
It reconciles the already-supported backend rejection with a resolution action
instead of re-implementing the rejection.

## Final location

Fork-only classification: this is core agent settings behavior. Final location is
**① an upstream PR proposal candidate** (kept as a fork temporary patch bundle
until upstream accepts). Upstream writes are out of scope; the value and
preparation are handed to the dedicated upstream task (93c6b4d0/W091) separately.

## Design-card consistency

This is a user-visible control change and stays within the design card's
invariant 3 ("사용자 가시 제어"): the administrator chooses the outcome and can
cancel. It mints no caller identifier (invariant 1), keeps the recovery line
independent (invariant 2), touches only the target profile and its referencing
dynamic routes within one workspace (invariants 4 and 5), and does not write
across workspaces, so it does not conflict with REQ-OFFICE-XWS-004.3.

Owning index pointers:
`_Prj_Knowledge/p200_index/설계카드.md` invariants table; `설계색인.md`
"AIWS 통합 설계" row "원칙 (AIWS=정본, Kandev=실행)" →
`260925_Kandev_AIWS_통합설계_초안_ver01.md` §2 원칙 5 (사용자 가시 제어). No
invariant is contradicted; no deviation record is needed.

## Confirmed evidence and assumptions

Confirmed against the admitted fork base (read-only inspection in this design
turn):

- Rejection: `controller/profile_crud.go` `UpdateProfile` (disable gate) and
  `prepareProfileDeletion` return `ErrProfileInUseDetail{DynamicProfiles}`.
- Delete handler serializes `dynamic_profiles`; the update handler serializes it
  too, but the frontend ignores it.
- Frontend gaps: `DeleteProfileResult` has no `dynamicProfiles`; the delete
  conflict dialog does not render them; `useProfileSave` handles only
  `utility_agents`; the list toggle shows a generic toast.
- Tier model: `controller/dynamic_selection.go` defines
  `join_previous`/tier-head ownership and `mode`/`on_failure`; the first row is
  always a head.

Decisions (ROOT-corrected and approved):

- **Delete-entry choices.** The delete entry keeps two positive choices plus
  Cancel: (a) disable the candidate in every referencing route and disable the
  profile instead of deleting it (`operation=disable`,
  `action=disable_candidate`; the profile row is preserved and the copy states
  that the profile is not deleted), and (b) remove the candidate from every
  referencing route and then delete the profile (`operation=delete`,
  `action=remove_candidate`). `operation=delete` with
  `action=disable_candidate` is never accepted, so a disabled reference never
  outlives a deleted row. This preserves the owner's approved choices ① profile
  disable and ② "(삭제면 제거 후 삭제)".
- **Transaction re-read.** The server re-reads the complete current reference
  set inside the transaction; a new or removed reference, or any changed
  dynamic-profile version since the dialog snapshot, rejects the whole request
  with the existing conflict contract rather than partially updating or ignoring.
- **Dedicated endpoint over force.** The resolution uses a dedicated request
  option, not `force`, so it cannot skip routing-tier or utility checks. A
  co-occurring utility conflict keeps its existing separate confirmation.
- No production code or permanent test is changed in this design turn.

## Scope

### In scope

- Backend: extended reference projection, the resolution endpoint, tier-head
  inheritance, and the atomic single-transaction mutation across all referencing
  dynamic profiles plus the target profile.
- Frontend: typed `dynamicProfiles` conflict parsing, the resolution dialog, and
  its wiring into the editor save, the list-row toggle, and the delete flow,
  desktop and phone, with localized copy in all catalogs.
- Companion unit tests beside the changed logic.

### Out of scope

- Auto-resolving routing-tier, utility-agent, session, watcher, or automation
  references.
- Changing dynamic router selection, classification, or health.
- New permission model, new settings screen, or bulk dynamic editor.
- New integration/E2E suites, collection paths, CI changes, or gates.
- Upstream writes; only the value/preparation is handed to the dedicated task.

## Technical approach

1. **Reference projection.** Extend `models.DynamicProfileReference` (or a
   route-level sibling) with `Version` and the referencing `Routes`
   (`position`, `tier`, `enabled`). Extend
   `ListDynamicProfileReferencesByExecutionProfile` to return them and propagate
   them through `controller.listDynamicProfileReferences` and both handlers.
2. **Resolution controller.** Add
   `controller/dynamic_reference_resolution.go` with pure builders for
   `disable_candidate` and `remove_candidate`, including position renumbering and
   tier-head inheritance, then `Controller.ResolveDynamicReferences` to validate
   versions and call the store.
3. **Atomic store.** Add an optional store extension
   `ResolveDynamicReferencesAndUpdateProfile` /
   `ResolveDynamicReferencesAndDeleteProfile` that re-reads the current reference
   set inside the transaction, CASes each dynamic profile version, writes the new
   routes, and updates/deletes the target profile in the same writer transaction.
   Any new/removed reference, version mismatch, or failure rolls the whole
   transaction back.
4. **Handler.** Add
   `POST /api/v1/agent-profiles/:id/dynamic-reference-resolution` with the
   request/response contract in the design. Keep the existing PATCH/DELETE 409
   responses.
5. **Frontend.** Extend the error types and parsing, add the resolution dialog,
   and wire the editor save, list toggle, and delete hooks.
6. **i18n.** Add the new keys to each catalog, including `ko`.

## ASCII UI preview

UI-01: Dynamic route reference resolution confirmation.
Entry points: profile editor header enabled toggle, Agents list-row enabled
toggle, and profile delete. Data names below are illustrative; copy is localized.

```text
Desktop (AlertDialog):
+--------------------------------------------------------------+
| Disable "gpt-balanced"?                                      |
|                                                              |
| This profile is a candidate in these dynamic profiles:       |
|   Frontier        tier 1 (candidate 1)                       |
|   Cost Optimizer  tier 2 (candidate 3)                       |
|                                                              |
| Choose what happens to those references:                     |
|   ( ) Disable the candidate in every dynamic profile         |
|       (keeps the reference, marked ineligible)            [1]|
|   ( ) Remove the candidate from every dynamic profile        |
|       (deletes the reference row)                         [2]|
|                                                              |
|                        [Cancel]  [Apply and disable]         |
+--------------------------------------------------------------+
clicked [1] selected -> "Apply and disable"

Delete variant: same list, two positive choices plus Cancel. Choice (1)
"Disable candidate and disable this profile instead of deleting it" (the
profile row is preserved; the copy states it is not deleted). Choice (2)
"Remove candidate from routes and delete this profile".

Phone (bottom inset sheet, one scroll owner, 44px rows):
+--------------------------------------+
| Disable "gpt-balanced"?              |
| Frontier        tier 1 (candidate 1) |
| Cost Optimizer  tier 2 (candidate 3) |
| [ ] Disable candidate in routes   [1]|
| [ ] Remove candidate from routes  [2]|
| [Cancel]        [Apply and disable]  |
+--------------------------------------+
```

Structural requirements: the reference list names profile + tier/position; two
positive choices plus Cancel on disable and one on delete; the least destructive
positive choice is the Enter default; the phone container keeps one scroll owner,
safe-area padding, and at least 44px touch targets. ASCII spacing is
illustrative. Maps to AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.1, .2, .3, .7, .9.

## Tests

Companion unit tests beside the changed logic (no new integration/E2E suites):

| Planned test | Boundary | Acceptance mapping |
| --- | --- | --- |
| disable_candidate sets every referencing route row `enabled=false` and disables the target | Go: resolution builder + controller | .2 |
| remove_candidate removes rows, renumbers, and inherits head `mode`/`on_failure` into the next tier row | Go: resolution builder table | .3, .4 |
| stale version on any referencing dynamic profile rejects the whole request with no rows changed | Go: controller + fake store | .6 |
| a new/removed reference or changed reference set since the snapshot rejects the whole request | Go: controller + fake store | .6 |
| one atomic repository call writes all referencing dynamic profiles plus the target and rolls back on any failure | Go: store adapter when implemented | .5 |
| delete entry disable-instead runs `operation=disable` and preserves the row; `delete`+`disable_candidate` is rejected | Go: controller | .2, .3 |
| routing-tier/utility conflicts are not bypassed and a lookup failure blocks | Go: controller | .7 |
| `dynamic_profiles` is parsed and the resolution action is called from editor/list/delete | TS: actions + hooks | .1, .9 |
| the dialog offers the documented choices and Cancel and localizes copy | TS: component test | .2, .8 |

A user-facing requirement needs end-to-end evidence, provided by the installed
replacement readback in the plan's verification strategy, not a new permanent E2E
suite.

## Work orders

- [ ] [Task 01: Backend dynamic-reference resolution contract](task-01-backend-resolution.md) (`pending`, sequential).
- [ ] [Task 02: Frontend resolution surfaces](task-02-frontend-resolution.md) (`pending`, depends on Task 01).

## Verification strategy and results

Design checkpoint (this turn): Python catalog/spec validation and spec lint only.
Implementation (after ROOT's explicit release):

- Backend: `make -C apps/backend test` scoped to
  `./internal/agent/settings/controller/... ./internal/agent/settings/handlers/... ./internal/agent/settings/store/...`
  plus `go build`/`go vet` on the changed packages.
- Frontend: the changed vitest files, `pnpm run typecheck`, `pnpm run lint`,
  `pnpm run i18n:check`, `pnpm run i18n:ratchet` from `apps/web`.
- Installed replacement readback: with a throwaway test profile that is a
  candidate in a scratch dynamic profile, confirm path ① (disable candidate +
  disable profile) and path ② (remove candidate + disable/delete) through the
  real settings UI, then restore the test configuration to its original state.

## Risks

- Changing the shared `dynamic_profiles` conflict shape must stay backward
  compatible for consumers that read only `id`/`name`/`deleted`.
- Tier-head inheritance is easy to get wrong at a tier boundary; it must be
  table-tested for head removal, non-head removal, sole-row removal, and
  next-row-is-a-new-head.
- Reusing `force` would silently bypass utility/routing checks; the design must
  keep the dedicated resolution option.
- The multi-profile atomic write needs a real single-transaction store method;
  sequencing separate updates would violate AC .5.

## Results

Implemented on `feature/7fa884cd-adb7-4027-8-zp4` (fork base main), PR
https://github.com/dreampiler/kandev/pull/283, head commit `560baad567`.

- Backend: extended conflict projection (`version` + `routes`), the dedicated
  `POST /api/v1/agent-profiles/:id/dynamic-reference-resolution` handler, the
  `Controller.ResolveDynamicReferences` builders with tier-head inheritance, and
  the atomic store resolution methods.
- Frontend: typed `dynamic_profiles` parsing, `resolveDynamicReferencesAction`,
  and the shared resolution dialog wired into the editor save, list-row toggle,
  and delete flow, with copy in every catalog.
- Verification (all passed on this worktree): `go build ./...`; `go vet
  ./internal/agent/settings/...`; `golangci-lint run ./internal/agent/settings/...
  --new-from-rev=HEAD` (0 issues); `go test
  ./internal/agent/settings/{store,controller}/...` filtered to the dynamic
  resolution and delete cases; `tsc --noEmit`; `eslint` on the changed files;
  `vitest` on the changed tests (34 passing); `i18n:check` and `i18n:ratchet`.
- Not run: the full backend suite (the controller package suite does not
  terminate within the 10-minute command budget on this host) and the installed
  replacement UI readback. The store package full run has pre-existing Windows
  timestamp-ordering failures (`TestDuplicateAgentProfile_DetectsConcurrentChange`,
  `TestProfileMoveDoesNotCarrySavedRankToAnotherGroup`) that also fail on the
  base branch and are unrelated to this change.
- Follow-up verification item (reported to ROOT, not bypassed): installed
  replacement UI readback of path ① (disable candidate + disable profile) and
  path ② (remove candidate + disable/delete) with a throwaway test profile, then
  restore.

