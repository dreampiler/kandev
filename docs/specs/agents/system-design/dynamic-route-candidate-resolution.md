---
status: draft
system: agents
requirements:
  - REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001
---
# Dynamic Route Candidate Resolution System Design

## Purpose and boundaries

This design explains how Kandev lets an administrator resolve the dynamic
profiles that reference a concrete profile before disabling or deleting it. It
owns the resolution request contract, the atomic mutation of every referencing
dynamic route document plus the target profile, and the settings confirmation
that carries the administrator's choice.

Agents already owns the profile-in-use rejection and the dynamic routing
document. This design extends those boundaries rather than adding a new one:

- The existing rejection path (`ErrProfileInUseDetail` with
  `DynamicProfiles`) is unchanged and still runs first; this design adds the
  endpoint and UI that resolve the reported references.
- Provider selection, classification, and health live in
  [Dynamic Agent Routing Part 1](dynamic-agent-routing-01.md); this design only
  edits configuration rows.
- The shared dependency-confirmation contract for utility agents and routing
  tiers is owned by
  [ADR-2026-08-08-utility-profile-dependency-safety](../../../decisions/2026-08-08-utility-profile-dependency-safety.md)
  and is reused, not replaced.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001` (.1, .2, .8, .9) | [Components and responsibilities](#components-and-responsibilities), [Data and contracts](#data-and-contracts), [Frontend resolution surfaces](#frontend-resolution-surfaces) |
| `REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001` (.4) | [Tier-head inheritance](#tier-head-inheritance) |
| `REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001` (.3, .5, .6) | [Resolution control flow](#resolution-control-flow), [Persistence and atomicity](#persistence-and-atomicity) |
| `REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001` (.7) | [Security and dependency boundaries](#security-and-dependency-boundaries) |

## Current source and reconciliation

The rejection this design resolves already exists at the admitted base:

- `Controller.UpdateProfile` (`apps/backend/internal/agent/settings/controller/profile_crud.go`)
  returns `ErrProfileInUseDetail{DynamicProfiles: ...}` when a request sets
  `enabled=false` without `force` and `listDynamicProfileReferences` finds any
  referencing dynamic profile.
- `Controller.prepareProfileDeletion` returns the same detail for a non-force
  delete.
- `Controller.DeleteProfile` already lives with stale references when forced.
- `listDynamicProfileReferences` reads
  `store.DynamicProfileRepository.ListDynamicProfileReferencesByExecutionProfile`
  (`store/sqlite.go`), and `DynamicProfileReference` (`controller.go`) exposes
  only `id`, `name`, and `deleted`.

The defect is on the consumer side, not the rejection:

- `handlers/httpDeleteProfile` serializes `dynamic_profiles` in its 409, but
  `writeProfileUpdateError` serializes the dynamic list only for the update path
  and the frontend ignores it.
- The frontend `DeleteProfileResult` (`apps/web/app/actions/agents.ts`) has no
  `dynamicProfiles` field, `AgentProfileDeleteConflict`
  (`components/settings/agent-profile-delete-dialog.tsx`) never renders dynamic
  profiles, and `useProfileSave` (`components/settings/agent-profile-page-state.ts`)
  handles only `utility_agents`. The list-row toggle
  (`hooks/domains/settings/use-profile-enabled-toggle.ts`) swallows the 409 as a
  generic toast.

This design does not re-decide or re-implement the rejection; it reconciles the
supported contract (typed conflict detail) with a resolution action and a
surface that presents it.

## Components and responsibilities

Paths are relative to `apps/backend/` unless noted.

| Component | Responsibility |
| --- | --- |
| `internal/agent/settings/controller/profile_crud.go` | Owns `UpdateProfile` and `DeleteProfile`. Adds `ResolveDynamicReferences`, which validates versions, builds each referencing dynamic profile's new route document, and calls the atomic repository method. Existing rejection paths remain. |
| New `internal/agent/settings/controller/dynamic_reference_resolution.go` | Pure helpers: build new routes for `disable_candidate` and `remove_candidate`, renumber positions, and compute [tier-head inheritance](#tier-head-inheritance). Keeps `profile_crud.go` within its size limit. |
| `internal/agent/settings/store/sqlite.go` and `store.go` | Extends the reference reader to include route positions, versions, and enablement, and adds the atomic resolution repository method. |
| `internal/agent/settings/models/dynamic.go` | Extends `DynamicProfileReference` (or adds a route-level reference model) with the referencing route positions, tier, and dynamic-profile version. |
| `internal/agent/settings/dto/dto.go` | Adds the resolution request DTO and the extended reference projection. |
| `internal/agent/settings/handlers/profile_handlers.go` | Adds the resolution route, keeps the existing 409 payload, and serializes the extended reference shape. |
| `apps/web/app/actions/agents.ts` | Adds `resolveDynamicReferencesAction`; extends `DeleteProfileResult` and error parsing with `dynamicProfiles`. |
| `apps/web/lib/types/agent-profile-errors.ts` | Adds the `DynamicProfileReference` wire type. |
| `apps/web/components/settings/agent-profile-delete-dialog.tsx` | Adds the resolution dialog (or a sibling component) offering the choices and Cancel, reusing the existing alert-dialog conventions. |
| `apps/web/components/settings/agent-profile-page-state.ts` | Wires the editor save and delete hooks to the dialog and the resolution action. |
| `apps/web/hooks/domains/settings/use-profile-enabled-toggle.ts` | Wires the list-row toggle to the dialog and the resolution action. |
| `apps/web/src/locales/<locale>/agents.json` | New keys in every catalog, including `ko`. |

## Data and contracts

### Extended reference projection

`GET`/`PATCH`/`DELETE` conflict responses keep the top-level shape and extend
`dynamic_profiles` from `{id, name, deleted}` to include the referencing route
detail:

```json
{
  "dynamic_profiles": [
    {
      "id": "<dynamic profile id>",
      "name": "<dynamic profile name>",
      "deleted": false,
      "version": 7,
      "routes": [
        { "position": 0, "tier": 1, "enabled": true },
        { "position": 3, "tier": 2, "enabled": false }
      ]
    }
  ]
}
```

`version` is the dynamic profile document's optimistic version at read time and
is the value the resolution request must echo. `routes` names each position and
its 1-based tier so the dialog can say which grade contains the profile.

### Resolution request

A dedicated endpoint keeps the atomic multi-profile mutation and version echo out
of the generic profile PATCH/DELETE bodies:

```
POST /api/v1/agent-profiles/:id/dynamic-reference-resolution
{
  "operation": "disable" | "delete",
  "action": "disable_candidate" | "remove_candidate",
  "versions": { "<dynamic profile id>": 7, "...": 4 }
}
```

- `operation=disable` sets the target profile `enabled=false`; `operation=delete`
  deletes the target profile.
- `action=disable_candidate` keeps each candidate row with `enabled=false`;
  `action=remove_candidate` removes each candidate row and renumbers.
- `versions` must contain exactly the referencing dynamic profiles reported in
  the conflict. An unknown or missing id is rejected.
- Success returns the target profile DTO (`disable`) or `{ "success": true }`
  (`delete`), mirroring the existing PATCH and DELETE responses.

The confirmation's display choices map to operation/action pairs by entry point:

| Entry point | Display choice | Effective request |
| --- | --- | --- |
| Disable | disable candidate | `operation=disable`, `action=disable_candidate` |
| Disable | remove candidate | `operation=disable`, `action=remove_candidate` |
| Delete | disable instead of deleting | `operation=disable`, `action=disable_candidate` (profile row preserved; copy must say the profile is not deleted) |
| Delete | remove then delete | `operation=delete`, `action=remove_candidate` |

`operation=delete` with `action=disable_candidate` is rejected: a disabled
reference must not outlive the profile row it points at
(`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.3`).

### Dynamic document contract

Each referencing dynamic profile is written as a whole versioned document using
the existing `models.DynamicAgentProfile` plus `[]models.DynamicAgentRoute`
shape and the existing `selection`/`tier` policy document. This design changes no
policy field, default, or validation rule.

## Tier-head inheritance

The route list is ordered by `position`. A row is a **tier head** when it is the
first row or its `policies.selection.join_previous` is false; a joined row shares
the head's tier and owns only its model options. Removing a candidate must not
silently drop or reshape a tier.

When `remove_candidate` removes row `i`:

1. Remove the row and renumber the remaining rows so positions stay contiguous.
2. If the removed row was a tier head and the row that followed it (at old
   position `i+1`) joined it, that following row becomes the new head: set its
   `selection.join_previous=false` and copy the removed head's
   `selection.tier` (`mode`, `on_failure`, and tier-scoped fields) onto it. Its
   own model settings are preserved.
3. If the removed row was not a head, or the following row was itself a head, no
   inheritance occurs; the empty tier simply disappears.

This never merges two tiers, never promotes a non-adjacent row, and never changes
the relative order of the remaining candidates
(`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.4`). The transformation runs after the
existing `validateDynamicCandidates`/selection normalization so an inherited head
is re-validated under the normal rules.

## Resolution control flow

1. The administrator attempts the disable or delete through the existing
   surface. `UpdateProfile`/`DeleteProfile` runs its existing checks and returns
   `ErrProfileInUseDetail` with the dynamic references plus the extended route
   detail and versions.
2. The surface opens the resolution confirmation and lists each referencing
   dynamic profile and the tier/position of the candidate. Routing-tier and
   utility-agent conflicts, when present, keep their own existing confirmation
   or block and are shown alongside or in sequence, not auto-resolved.
3. On a positive choice, the surface calls the resolution endpoint with the
   operation, the action, and the echoed versions.
4. `Controller.ResolveDynamicReferences`:
   - re-reads the target profile and, inside the transaction, re-reads the
     complete current reference set,
   - verifies the reference set is unchanged and every echoed version matches the
     current version (fail closed on any new reference, removed reference, or
     version mismatch) before any write,
   - builds each new route document in memory,
   - calls the atomic repository method to write all referencing dynamic
     profiles and the target profile (or delete it) in one transaction,
   - for `operation=delete`, runs the existing delete side effects in their
     existing order around the row mutation (automations disabled before the row
     is removed, watchers disabled after).
5. The surface publishes the returned target profile (or removes it for delete)
   and clears the pending enabled change.

Cancellation at any step changes nothing.

## Failure and recovery

- **Version or reference-set conflict:** if the reference set changed or any
  echoed dynamic-profile version is stale, the whole request is rejected with the
  existing conflict contract and no rows change
  (`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.6`). The surface offers a refresh
  (re-attempt the original disable/delete to obtain a fresh conflict).
- **Dependency lookup failure:** a failure reading references or dynamic
  documents blocks the change; it is never treated as "no reference"
  (`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.7`).
- **Partial failure:** the transaction rolls back every dynamic profile and the
  target row together (`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.5`). The target
  profile is never left disabled while a referencing route still points at an
  enabled candidate, and a delete never leaves a candidate pointing at a removed
  row.
- **Invalid request:** a missing action, an unknown/extra version id, a
  `delete`+`disable_candidate` combination, or a profile with no dynamic
  references is rejected with a field-addressable 400 before any write.

## Persistence and atomicity

The resolution adds an optional store extension, mirroring the existing
`AtomicDynamicProfileRepository` pattern in `store/store.go`:

```
ResolveDynamicReferencesAndUpdateProfile(ctx, target *models.AgentProfile, enabled *bool, resolutions []DynamicProfileResolution) error
ResolveDynamicReferencesAndDeleteProfile(ctx, targetID string, resolutions []DynamicProfileResolution) error
```

`DynamicProfileResolution` carries the dynamic profile id, expected version, the
new `models.DynamicAgentProfile` version, and the new routes. The SQLite and
PostgreSQL implementations run the reference CAS, the route replacement, and the
target profile update/delete inside one writer transaction, so a failure at any
step commits nothing. The existing `kandev_meta`/migration rules are unaffected:
no schema change is required because the route and version columns already exist.

## Frontend resolution surfaces

All three entry points reach the same dialog and the same action:

- **Profile editor save:** `useProfileSave` gains a `dynamic_profiles` 409 branch
  (in addition to the existing `utility_agents` branch). Confirming the dialog
  calls the resolution action and publishes the returned profile through the
  existing `publishProfileSaveResponse` path.
- **Profile list-row toggle:** `useProfileEnabledToggle` gains the same branch
  and merges the returned profile through `applyEnabledProfileUpdate`.
- **Delete flow:** `deleteAgentProfileError` parses `dynamic_profiles`, the
  conflict type carries them, and `useProfileDelete` opens the resolution dialog
  instead of the delete conflict dialog when the conflict is dynamic-only. On the
  delete entry the dialog keeps two positive choices plus Cancel: disable the
  candidate and disable the profile instead of deleting it (mapped to
  `operation=disable`, with copy that states the profile is not deleted), and
  remove the candidate then delete the profile. A conflict that also has hard
  blockers (routing tiers) keeps the existing non-proceedable dialog.

The dialog reuses the existing alert-dialog and mobile confirmation primitives
(`AgentProfileDeleteConflictDialog`, `MobileActionConfirmation`,
`InlineConfirmActions`). It has two positive choices plus Cancel; the recommended
or least-destructive choice (disable the candidate) is the Enter default via
`data-dialog-default-action`. On phone it keeps one scroll owner, a 44px minimum
touch target, and the existing focus-return behavior. All new copy is added
through the `t()`/`Trans` catalog contract in every catalog including `ko`
(`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.8`, `.9`).

Desktop and phone share the same state, validation, and action; only the
container presentation differs.

## Security and dependency boundaries

- The resolver uses the same administrator settings permission as profile
  disable/delete; it adds no new permission model and no new route visibility
  (`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.8`).
- Routing-tier references remain a hard blocker; utility-agent, session, watcher,
  and automation references keep their existing confirmations. The resolution
  action never sets `force`, so it cannot skip those checks by accident
  (`AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.7`).
- The resolver touches only the target profile and the dynamic profiles that
  reference it by execution-profile id; it never reads or writes another
  workspace's Office-scoped rows and does not conflict with the cross-workspace
  control-write prohibition in
  [REQ-OFFICE-XWS-004.3](../../office/requirements/office-cross-workspace-access.md).

## Observability

No new metric is required. The existing structured logger records the resolution
outcome with the target profile id, the action, and the count of referencing
dynamic profiles; identifiers stay out of metric labels. This is a bounded
configuration mutation surfaced to the administrator, not background work.

## Related decisions

- [ADR-2026-08-08-utility-profile-dependency-safety](../../../decisions/2026-08-08-utility-profile-dependency-safety.md)
- [ADR-2026-08-13-dynamic-agent-profile-routing](../../../decisions/2026-08-13-dynamic-agent-profile-routing.md)
