---
id: "01-selection-contract"
title: "Persist dynamic tier and model selection settings"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-TIER-SELECTION-001
  - REQ-AGENTS-TIER-SELECTION-002
acceptance_criteria:
  - AC-AGENTS-TIER-SELECTION-001.2
  - AC-AGENTS-TIER-SELECTION-001.3
  - AC-AGENTS-TIER-SELECTION-001.4
  - AC-AGENTS-TIER-SELECTION-002.1
  - AC-AGENTS-TIER-SELECTION-002.2
  - AC-AGENTS-TIER-SELECTION-002.3
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
---

# Task 01: Selection Settings Contract

## Summary

Implement the additive row selection document and end-to-end configuration
round trip. Default/legacy routing remains unchanged; the settings API can
persist future tier behavior without losing failure policies.

## In scope

Selection types/codec, head ownership, server validation, omission preservation,
atomic persistence, duplication, dynamic resolver parsing and web normalization.
Follow the design's group mutation normalization on legacy-client reorder.
Persist the profile continuity preference in a default-true column on the
existing dynamic-profile record, with SQLite/PostgreSQL migration parity and
omitted-update preservation, independently of whether any candidate rows exist.

## Out of scope

Runtime ranking, provider fetch changes, rendered UI, operational values and
any deployment. Do not add a profile-level mode or eager migration.

## Acceptance

1. Empty/legacy/version-1 documents and additive selection documents read and
   round-trip without losing transient/hard/unclassified settings.
2. Head ownership, join validity and manual window validation fail atomically;
   omitted fields preserve saved selections, including duplicate, unrelated edits
   and clearing/repopulating the list without losing the keep-model preference.
3. Version conflicts and the settings lock preserve saved state and expose the
   existing recovery path. Runtime consumes typed metadata without changing
   failure policy evaluation.

## Verification

From `apps/backend/`:

```powershell
go test ./internal/agent/settings/controller ./internal/agent/settings/store -run 'Test(Dynamic|ValidateDynamic|NormalizeDynamic|UnclassifiedPolicy)' -count=1 -timeout=10m
go test ./internal/agent/runtime -run 'Test.*Dynamic' -count=1 -timeout=10m
```

After the plan's one-time install, from `apps/web/`:

```powershell
pnpm exec vitest run lib/api/domains/agent-profile-normalize.test.ts
pnpm run typecheck
```

Extend existing `dynamic_profile_test.go`, `sqlite_dynamic_profile_test.go`,
`dynamic_resolver_test.go` and normalizer tests with `TestDynamicTierSelectionRoundTrip`
and equivalent transport cases. See the plan's acceptance matrix for exact cases.
Do not mark success if the new cases are absent or the filter selects no tests.

## Files likely touched

- `apps/backend/internal/agent/settings/dto/dto.go`
- `apps/backend/internal/agent/settings/controller/dynamic_policy.go`
- `apps/backend/internal/agent/settings/controller/profile_crud.go`
- Proposed `apps/backend/internal/agent/settings/controller/dynamic_selection.go`
- `apps/backend/internal/agent/settings/store/sqlite_dynamic_profile_test.go`
- `apps/backend/internal/agent/settings/store/sqlite.go`
- `apps/backend/internal/agent/settings/models/dynamic.go`
- `apps/backend/internal/agent/runtime/dynamic_resolver.go`
- `apps/backend/internal/agent/runtime/dynamic/types.go`
- `apps/web/lib/types/agent-profile.ts`
- `apps/web/lib/api/domains/agent-profile-normalize.ts`

## Dependencies and risks

Prerequisites: earlier replacement completed, design package reviewed, and
scoped `AGENTS.md` re-read. The owner has already requested implementation.
Preserve DTO presence versus explicit
defaults. An older server cannot be promised lossless downgrade behavior.

## Results

Pending. No production or permanent test changes in the design turn.
