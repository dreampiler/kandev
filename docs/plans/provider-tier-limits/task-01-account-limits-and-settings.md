---
status: done
---

# Account limits and settings

## Outcome and scope

Implement configuration, usage/pace admission, classified observations, exact OpenRouter split, and responsive settings together. Owned areas are `agent/usage`, `agent/runtime/dynamic`, the provider-limit controller, `backendapp` composition, task SQLite persistence, the existing settings card/API/hooks, locale catalogs, and public agent documentation.

Excluded: route reordering, model choice, idle-timeout changes, production writes, runtime builds/swaps, new approval gates, new integration/E2E/CI or recurring test infrastructure, and unrelated Windows failures. Depends on [the plan](plan.md) and fork PR #272.

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

No independent review, merge, installation, or natural-error learning readback has occurred. PR2 will change learning to owner > learned > defaults, add scope-specific daily counts, and learn groups only from unambiguous classified-limit evidence; congestion and unclassified failures will not learn. The existing response-count estimate provenance remains visible. The overall owner outcome remains outstanding.
