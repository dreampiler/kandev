---
status: in progress
---

# Account limits and settings

## Outcome and scope

Implement configuration, usage/pace admission, classified observations, exact OpenRouter split, and responsive settings together. Owned areas are `agent/usage`, `agent/runtime/dynamic`, the provider-limit controller, `backendapp` composition, task SQLite persistence, the existing settings card/API/hooks, locale catalogs, and public agent documentation.

Excluded: route reordering, model choice, idle-timeout changes, production writes, runtime builds/swaps, new approval gates, new integration/E2E/CI or recurring test infrastructure, and unrelated Windows failures. Depends on [the plan](plan.md) and the reviewed native baseline `origin/main 07fa2d5e7`.

## Contract references

- `REQ-COSTS-TIER-LIMITS-001`.
- `AC-COSTS-TIER-LIMITS-001.1`, `AC-COSTS-TIER-LIMITS-001.2`, `AC-COSTS-TIER-LIMITS-001.3`, `AC-COSTS-TIER-LIMITS-001.4`, `AC-COSTS-TIER-LIMITS-001.5`, `AC-COSTS-TIER-LIMITS-001.6`.
- [System design](../../specs/costs/system-design/provider-tier-limits.md).

## Acceptance

1. Account/tier budgets persist, display sources, and change admission without changing order or interrupting running work.
2. Classified observations learn unknown limits. Exact OpenRouter evidence overrides unknown-wording timing; unavailable evidence preserves it.
3. Desktop and phone edit/save the same settings through the normal settings entry point.

## ASCII UI preview

UI-01 from [the plan](plan.md#ascii-ui-preview): desktop has three tier columns; phone has a visible tier navigator and one editor, followed by profile mapping and Save/status. The page owns scrolling and source values stay visible beside each input.

## Verification

Run only the affected package/file checks. Source-adjacent companion units cover override precedence and clearing, independent learned persistence, final-turn admission/last-slot serialization, and partial settings-save responses. Existing cases cover monthly resets, paid blocks and keep-model continuity.

- Focused go test -trimpath -race -p 2 -timeout 5m in usage, dynamic, SQLite, backendapp, orchestrator, provider-limit handlers and runtime with explicit provider-limit/admission filters.
- Build and vet changed Go packages; golangci-lint run --new-from-rev=4830d61604 in changed packages.
- Focused provider-settings Vitest, web typecheck, changed-file ESLint and pnpm run i18n:check.
- Disposable desktop/phone component capture and save/readback, preserving shared drafts and existing reset/block controls. No permanent E2E collection.
- Existing spec catalog/lint and public-document validators; normal commit hooks without bypass.

## Current execution results

Base `4830d61604` was incorporated by ordinary fast-forward merge. The preserved 61-file patch applied without conflicts. Historical predecessor test/lint claims are intentionally removed: they were not repeated evidence for this snapshot.

The provider-limit save hook lost account editor state when a legacy reset/block response omitted accounts. A companion test failed on that omission, then passed after preserving the current account list. The account-save counterpart verifies that monthly reset and paid block values remain unchanged. Focused Vitest passed five cases. Localization checks passed. Initial changed-code Go lint passed with zero issues after the two complexity sites were extracted into smaller helpers; configuration and lint suppressions were unchanged.

The focused Go race run passed dynamic, SQLite, backendapp, orchestrator and provider-limit handlers. Runtime compiled but the filter selected no runtime cases. Its original aggregate exit was 1 because the new IANA-time-zone unit fixture lacked the database embedded by `cmd/kandev`. The fixture now embeds the same database; a targeted usage/dynamic race rerun passed with exit 0. Changed Go packages built with exit 0. No real provider was called.

The standard web typecheck first exited 134 at its heap limit. The same TypeScript check using a process-local 8 GiB heap passed with exit 0; no configuration changed. Changed-file ESLint and locale checks passed. Companion Vitest and the existing card test passed five cases. Desktop (1440px), phone (393px) and narrow fine-pointer (767px) component checks preserved drafts across viewport/tier changes and saved three requests. The phone input was 44px high with no document horizontal overflow. The existing paid block save preserved the account editor after its partial response. These use a mocked settings transport, not an installed-runtime or backend persistence E2E proof.

The specification catalog and specification lint passed. Public-document coverage failed with `coverage.json does not account for MCP tool: report_to_handoff_source_kandev`, the same unrelated failure recorded by the predecessor. It was not re-probed on the base or presented as PASS.

Changed Go package vet passed with exit 0. Final changed-code lint found one repeated waiting-state literal after companion coverage was added; admission now reuses the existing waiting-state constant. The dynamic race rerun and scoped lint passed with exit 0. A shared-host lint lock refused one call; the serial runner then passed without disabling the lock. Normal commit hooks are recorded in the PR handoff.

Commands were run from `apps/backend` or `apps/web` as appropriate:

```text
go test -trimpath -race -p 2 -timeout 5m ./internal/agent/usage ./internal/agent/runtime/dynamic ./internal/task/repository/sqlite ./internal/backendapp ./internal/orchestrator ./internal/agent/settings/handlers ./internal/agent/runtime -run 'TestProviderTier|TestProviderLimit|TestOpenRouter|TestManualWindowUsageForProfiles|TestUsageLimitObservations|TestKeepModel|TestReevaluateForNewTurn' -count=1
  Exit 1: time-zone fixture failure; remaining selected packages passed.
go test -trimpath -race -p 2 -timeout 2m ./internal/agent/usage ./internal/agent/runtime/dynamic -run 'TestProviderTier|TestOpenRouter' -count=1
  Exit 0 after matching the application's embedded time-zone database.
go test -trimpath -race -timeout 2m ./internal/agent/runtime/dynamic -run TestProviderTier -count=1
  Exit 0 after reusing the waiting-state constant.
go build -p 2 ./internal/agent/runtime/dynamic ./internal/agent/runtime ./internal/agent/settings/controller ./internal/agent/usage ./internal/backendapp ./internal/orchestrator ./internal/persistence/requiredstores ./internal/task/repository/sqlite
  Exit 0.
go vet -p 2 ./internal/agent/runtime/dynamic ./internal/agent/runtime ./internal/agent/settings/controller ./internal/agent/usage ./internal/backendapp ./internal/orchestrator ./internal/persistence/requiredstores ./internal/task/repository/sqlite
  Exit 0.
golangci-lint run --allow-serial-runners --new-from-rev=4830d61604 --timeout=5m --concurrency=2 ./internal/agent/runtime/dynamic
  Exit 0, zero issues. Other changed packages had no findings in the preceding changed-code run.
pnpm exec vitest run hooks/domains/settings/use-provider-limits.test.ts components/settings/provider-limits-card.test.tsx
  Exit 0, five cases.
node --max-old-space-size=8192 node_modules/typescript/bin/tsc --noEmit
  Exit 0 after the standard typecheck exited 134 at its heap limit.
```

At the initial PR handoff, independent review, merge, installation, and natural-error learning readback were outstanding. PR2 will change learning to owner > learned > defaults, add scope-specific daily counts, and learn groups only from unambiguous classified-limit evidence; congestion and unclassified failures will not learn. The existing response-count estimate provenance remains visible. The overall owner outcome remains outstanding.

## PR1 review remediation (2026-10-14)

The reviewed PR1 head was `dbf97a73cc`. Fork main `c6b90cd1c7` was incorporated by an ordinary merge, with normal hooks, before remediation. The six reproduced defects were fixed without changing provider values, route ordering, settings permissions, or the PR2 learning scope.

| Finding  | Disposition                                                                                                                                                                                                                                                                                 |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| fd358b0e | Not reproduced, nonblocking. `base_schema.go:48` calls `initUsageLimitObservationsSchema` before `runMigrations` at line 63. Fresh initialization, observation-field round-trip/replay, and an existing-row legacy-schema upgrade all passed. No production migration change was necessary. |
| 33f6d1c5 | Preserve each window's learned provenance and its 5% reserve. An explicit owner allowance replaces learned provenance.                                                                                                                                                                      |
| d8e25beb | Atomically claim the lifecycle session before reading its route for admission. Busy/inactive claims do not read admission inputs; failed admission restores the claim.                                                                                                                      |
| 1f7dfa4e | Serialize final capacity reads and claims with a separate admission mutex, perform the read outside the engine state mutex, and recheck generation under the state mutex.                                                                                                                   |
| df4cf385 | Unit/reset edits retain a blank inherited allowance. Backend window merging preserves the inherited allowance and provenance when the owner changes rules only.                                                                                                                             |
| 9b3810ed | Publish the authoritative server provider snapshot after account saves. Missing saved-account evidence rejects the save. Legacy responses that omit accounts still preserve account editors.                                                                                                |
| afa4b2ee | Supply an editable empty tier state when a response omits a tier.                                                                                                                                                                                                                           |

Red evidence covered lost window provenance, lost rule-only inheritance, engine-lock blocking during final admission, pre-claim session reads, and both save-response defects. The first blocked-admission fixture also blocked initial selection; it timed out in fixture setup, was corrected to block final admission only, and then reproduced the intended lock defect.

Targeted green checks:

```text
go test -trimpath -race -p 2 -timeout 2m ./internal/agent/usage ./internal/agent/runtime/dynamic ./internal/orchestrator ./internal/task/repository/sqlite -run 'TestProviderTier|TestUsageLimitObservationsRoundTrip' -count=1
  Exit 0; all four selected packages passed.
go test -p 2 -timeout 2m ./internal/task/repository/sqlite -run '^TestUsageLimitObservationsLegacyUpgrade$' -count=1
  Exit 0; old-schema rows survived the actual initialization/upgrade path. Process-local GOMAXPROCS=2 and GOFLAGS=-p=2 were used. The preceding race variant stalled before an observed test-binary launch and was stopped; it is not a PASS claim.
go build -p 2 ./internal/agent/usage ./internal/agent/runtime/dynamic ./internal/orchestrator ./internal/task/repository/sqlite
  Exit 0.
go vet -p 2 ./internal/agent/usage ./internal/agent/runtime/dynamic ./internal/orchestrator ./internal/task/repository/sqlite
  Exit 0.
golangci-lint run --allow-serial-runners --new-from-rev=dbf97a73cc3bc43ccb401861632687b154eab575 --timeout=8m --concurrency=2 ./internal/agent/usage ./internal/agent/runtime/dynamic ./internal/orchestrator ./internal/task/repository/sqlite
  Exit 0; zero issues. The first 5-minute run reported zero issues but exited 1 on timeout and was not treated as PASS.
golangci-lint run --allow-serial-runners --new-from-rev=dbf97a73cc3bc43ccb401861632687b154eab575 --timeout=8m --concurrency=2 ./internal/task/repository/sqlite
  Exit 0; zero issues after adding the legacy-upgrade companion case.
pnpm exec vitest run hooks/domains/settings/use-provider-limits.test.ts lib/settings/provider-tier-limits.test.ts components/settings/provider-limits-card.test.tsx
  Exit 0; three files, nine cases.
node --max-old-space-size=8192 node_modules/typescript/bin/tsc --noEmit
  Exit 0.
```

Changed-file ESLint, `pnpm run i18n:check`, the specification catalog, and specification lint passed with exit 0. UI changes only normalize state and draft data in the existing shared desktop/phone components; layout, touch targets, scrolling, and navigation are unchanged, so targeted helper/hook and existing component coverage satisfy this remediation's mobile scope. Public documentation already describes blank inheritance and the learned reserve; no public contract or copy changed. Normal commit/push receipts belong in the controller handoff and task plan. Controller review of the new delta and merge authorization remain outstanding; this session does not merge or signal workflow completion.

## Selection/final-admission review remediation (2026-10-14)

Independent review `8c5105bd` completed the `dbf97a73cc...4a311e7e78` delta (43 files). The previous dispositions were accepted; new findings 128a3f32 and 604c6b5a exposed two interleavings introduced by the split admission/state locks. The owner contracts, values, and PR2 scope remain unchanged.

- Select, unclassified successor, interrupted successor, and final prompt admission now share the admission mutex before the state mutex. Selection capacity is sampled outside the state mutex; candidate ranking and resource-wait deadlines consume the same snapshot. Probe claims do no capacity I/O.
- Lifecycle atomic claim, route/incarnation revalidation, and failed-claim restoration run inside the admission callback. An unconfigured routing path keeps its direct atomic claim. This retains the earlier stale-route protection without exposing transient lifecycle claims to competing admissions.
- Companion red evidence: Select and final admission both succeeded and claimed capacity 2 against limit 1; concurrent lifecycle requests both returned resource-wait and admitted zero. The lifecycle fixture initially duplicated a workspace through a shared seeding helper and was corrected to create two sessions in the same disposable database.

Targeted race verification passed with exit 0 in both changed packages:

```text
go test -trimpath -race -p 2 -timeout 2m ./internal/agent/runtime/dynamic ./internal/orchestrator -run 'TestProviderTier|Test.*Unclassified|Test.*Interrupted|TestAccountBlock' -count=1
go build -p 2 ./internal/agent/runtime/dynamic ./internal/orchestrator
go vet -p 2 ./internal/agent/runtime/dynamic ./internal/orchestrator
golangci-lint run --allow-serial-runners --new-from-rev=4a311e7e784cf2114269d0417db38d676526552b --timeout=8m --concurrency=2 ./internal/agent/runtime/dynamic ./internal/orchestrator
```

Build, vet, and scoped lint also passed with exit 0, with zero lint issues. A single-case race check of `TestProviderTierLifecycleClaimRejectsChangedRoute` passed: changing the route immediately after atomic claim rejects dispatch, restores the session state, and preserves the replacement generation. Normal commit/push and the controller's `4a311e7e78...new head` review receipt belong in the task plan. No frontend, provider, operating-data, integration/E2E/CI, or permission change is part of this delta. The task remains unmerged, without a workflow completion signal.
