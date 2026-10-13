---
id: "01-backend-resolution"
title: "Backend dynamic-reference resolution contract"
status: done
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001
system_design: "../../specs/agents/system-design/dynamic-route-candidate-resolution.md"
---

# Task 01: Backend dynamic-reference resolution contract

## Outcome

The backend can resolve every dynamic profile that references a concrete profile
before that profile is disabled or deleted, in one atomic request, with tier-head
inheritance and optimistic-version fencing. The existing rejection paths are
unchanged; this adds the resolution path and the extended reference projection.

## In scope

- Extend the reference model and both 409 handlers with `version` and route
  detail (`position`, `tier`, `enabled`).
- Add `controller/dynamic_reference_resolution.go` with pure builders for
  `disable_candidate` and `remove_candidate` (renumbering and tier-head
  inheritance) and `Controller.ResolveDynamicReferences`. Map the delete entry's
  disable-instead choice to `operation=disable`, `action=disable_candidate`, and
  reject `operation=delete` with `action=disable_candidate`.
- Add the atomic store extension
  `ResolveDynamicReferencesAndUpdateProfile` /
  `ResolveDynamicReferencesAndDeleteProfile`. The resolver re-reads the complete
  current reference set inside the transaction and rejects the whole request when
  a reference was added or removed, or any dynamic-profile version changed, since
  the confirmation snapshot.
- Add `POST /api/v1/agent-profiles/:id/dynamic-reference-resolution`.
- Preserve the existing delete side-effect order (automations before the row,
  watchers after) around the atomic mutation.

## Exclusions

- No change to the existing rejection behavior or its 409 payload keys.
- No routing-tier/utility/session/watcher/automation auto-resolution.
- No routing-runtime, classification, or health change.
- No schema migration.

## Applicable IDs

- `REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001`
- `AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.1`, `.3`, `.4`, `.5`, `.6`, `.7`

## Design references

- `docs/specs/agents/system-design/dynamic-route-candidate-resolution.md`
  (Data and contracts, Tier-head inheritance, Resolution control flow,
  Persistence and atomicity, Security and dependency boundaries).

## Implementation acceptance conditions

1. A `remove_candidate` request against a profile referenced at a tier head
   removes the row, renumbers positions, and moves the head's `mode`/`on_failure`
   onto the next same-tier row, without merging or reordering other tiers.
2. A stale version, or a new/removed reference, on any referencing dynamic
   profile rejects the whole request with no rows changed, and a routing-tier or
   utility conflict is not bypassed.
3. All referencing dynamic profiles and the target profile are written in one
   transaction; no partial state is observable after a failure.

## Verification commands

- `make -C apps/backend test GOFLAGS=-trimpath`
  scoped with `-run` to the resolution and profile CRUD cases, against
  `./internal/agent/settings/controller/... ./internal/agent/settings/handlers/... ./internal/agent/settings/store/...`
- `go build ./...` and `go vet ./internal/agent/settings/...` from `apps/backend`.

## Likely files

- `apps/backend/internal/agent/settings/controller/profile_crud.go`
- `apps/backend/internal/agent/settings/controller/dynamic_reference_resolution.go` (new)
- `apps/backend/internal/agent/settings/controller/controller.go`
- `apps/backend/internal/agent/settings/store/store.go`
- `apps/backend/internal/agent/settings/store/sqlite.go`
- `apps/backend/internal/agent/settings/models/dynamic.go`
- `apps/backend/internal/agent/settings/dto/dto.go`
- `apps/backend/internal/agent/settings/dto/profile_contract.go`
- `apps/backend/internal/agent/settings/handlers/profile_handlers.go`
- `apps/backend/internal/backendapp/*` (route wiring)

## Dependencies

None; this task precedes Task 02.

## Results

Done. `models.DynamicProfileReference` carries `Version`/`Routes`; the store
projects route position/tier/enabled; `dynamic_reference_resolution.go` builds the
`disable_candidate`/`remove_candidate` documents with tier-head inheritance and
exposes `Controller.ResolveDynamicReferences`; the atomic
`ResolveDynamicReferencesAndUpdateProfile`/`...DeleteProfile` store methods
re-read the reference set and CAS every version in one transaction; the
`POST /api/v1/agent-profiles/:id/dynamic-reference-resolution` route is
registered. Companion tests
(`dynamic_reference_resolution_test.go`, `sqlite_dynamic_resolution_test.go`)
cover the builders, tier-head inheritance, conflict rollback, and the delete +
disable rejection. `golangci-lint --new-from-rev=HEAD` reports 0 issues.
