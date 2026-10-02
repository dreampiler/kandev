---
id: "01-selection-contract"
title: "Persist dynamic tier and model selection settings"
status: completed
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

Implemented on branch `kd/dyn-tier-selection`, base `c8d4c923c`, commits
`f0fbf9b3e` (design package) and `6dafe6bdd` (this work order).

### Delivered

- `dto.DynamicAgentPolicyDTO.Selection` is additive and optional, so an absent
  document keeps ordered routing. `DynamicAgentProfileDTO.KeepModelWhileRunning`
  is a pointer so an update can distinguish omission from an explicit false.
- `controller/dynamic_selection.go` owns the closed sets, the documented
  defaults, head ownership and the manual-window rules. Money allowances are
  parsed with `math/big.Rat`, so a subcent value is neither rounded nor treated
  as nonfinite, and a named timezone is validated with `time.LoadLocation`.
- `controller/dynamic_selection_group.go` indexes the saved document by concrete
  profile ID and rebuilds grouping. A join survives only between two rows that
  already shared a saved tier, so becoming neighbours never manufactures an
  edge, and each resulting fragment inherits its originating block's policy.
  Both worked examples in the design hold: `[A =B] [C =D]` moving B down yields
  `[A] [C] [B] [D]`, and `[A =B =C]` moving A down yields `[B =A =C]` with the
  same tier policy.
- `preserveDynamicSelection` resolves the keep-model pointer and the merge
  before validation, so an unrelated edit or a legacy client cannot erase joins.
- The single planned schema change is a default-true
  `dynamic_agent_profiles.keep_model_while_running` column, added through the
  existing `migrate.Apply` path with its error propagated rather than swallowed.
- `dynamic_resolver.go` splits the stored document into the failure-policy half
  and the additive selection half; failure-policy evaluation never sees
  selection. `dynamic/selection.go` adds the typed closed sets and the pure
  `DeriveTiers` helper.
- The web contract adds the selection types, camelCase normalization, and a
  write path that omits `selection` and `keep_model_while_running` when the
  draft never set them.

### Two defects the focused tests caught

- `DeriveTiers` had an inverted join condition that produced one tier per row
  and collapsed a legacy row set into a single tier.
- The first block-tracking pass in `readStoredDynamicSelection` clobbered a
  head's stored tier policy with the legacy default, so every head decoded as
  `order` regardless of what was saved.

### Verification actually run

| Check | Exit | Result |
| --- | --- | --- |
| `go test ./internal/agent/settings/controller -run 'Test(Dynamic\|ValidateDynamic\|NormalizeDynamic\|UnclassifiedPolicy)' -count=1 -timeout=10m` | 0 | controller and store pass |
| `go test ./internal/agent/runtime -run 'Test.*Dynamic' -count=1 -timeout=10m` | 0 | pass |
| `go test ./internal/agent/settings/controller -count=1 -timeout=10m -skip 'TestHostRuntimeUpdater'` | 0 | pass, whole package |
| `go test ./internal/agent/runtime/dynamic ./internal/agent/runtime -count=1 -timeout=10m` | 0 | pass |
| `golangci-lint run ./internal/agent/settings/... ./internal/agent/runtime/dynamic/... ./internal/agent/runtime` | 0 | 0 issues |
| `pnpm run typecheck` | 0 | pass |
| `pnpm exec eslint lib/types/agent-profile.ts lib/api/domains/agent-profile-normalize.ts lib/api/domains/agent-profile-selection.test.ts --max-warnings 0` | 0 | pass |
| `pnpm run i18n:check` | 0 | all catalogues complete, no new copy |
| `pnpm exec vitest run lib/api/domains/agent-profile-selection.test.ts lib/api/domains/agent-profile-normalize.test.ts` | 0 | 29 tests pass |
| `python scripts/list-docs.py validate` | 0 | 339 decisions, 1291 specifications |
| `python scripts/lint-spec-files.py --all` | 0 | pass |
| `git diff --check` | 0 | pass |

### Pre-existing failures, not regressions

Each was reproduced on the untouched base by stashing this work order.

- `settings/controller` `TestHostRuntimeUpdaterInvalidatesOnlyManagedNPMExecutionTree`
  hangs and times out the package; reproduced identically at base.
- `settings/store` `TestDuplicateAgentProfile_DetectsConcurrentChange` fails
  because the create and the concurrent bump land in the same timestamp tick;
  reproduced at base.
- `settings/handlers` fails only when the whole `settings/...` tree runs at once
  (cross-package SQLite "database is locked"); it passes in isolation.
- `routingerr`, `runtime/lifecycle` and `runtime/lifecycle/skill` fail on this
  Windows host for reasons unrelated to this change (an npx cache path asserted
  to live under `$HOME`, a symlinked skill directory, and an NPM-dependent test).

### Not delivered here

Runtime ranking, usage windows, manual ledger aggregation, durable transition
chains, the preview endpoints, the desktop and phone editors and the E2E
coverage are WO-02 and WO-03 and are not started. The settings contract this
work order adds is the foundation they consume.
