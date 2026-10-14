---
created: 2026-10-13
status: done
plan: ./plan.md
requirements:
  - REQ-AGENTS-CIRCUIT-RECOVERY-001
system_design:
  - ../../specs/agents/system-design/resource-circuit-policy-recovery.md
---

# Task 01: Reconcile and release resource circuits

## Scope and acceptance

Implement AC-AGENTS-CIRCUIT-RECOVERY-001.1 through .4 in dynamic runtime, agent settings, circuit persistence, overview identity, and web settings/formatting. Preserve Zen daily mapping, explicit per-day classification, account-hold semantics, settings authorization/interlock, and unrelated circuit keys. No production DB writes, build/swap, permanent E2E suite, or other lane worktree edits.

## Verification

- `go test -trimpath ./internal/agent/runtime/dynamic ./internal/agent/settings/controller ./internal/agent/settings/handlers ./internal/office/dashboard -count=1`
- `go test -trimpath ./internal/task/repository/sqlite -run 'TestCircuitPolicy|Test.*Circuit' -count=1`
- `pnpm exec vitest run app/office/overview/overview-format.test.ts hooks/domains/settings/use-resource-circuits.test.tsx lib/api/domains/resource-circuit-api.test.ts`
- Adjacent settings page/admin-gating Vitest, changed-file ESLint, web typecheck and i18n checks.
- Synthetic desktop/phone Vite preview: at least 44px release target, no horizontal overflow, releasing one row preserves the sibling.

## Results

- RED: OpenRouter first suspension was 23h instead of 5m; legacy snapshot omitted `updated_at`; timed circuits had no owner release; new settings POST returned 404; account-hold identity was empty; web account-hold scope label was empty.
- GREEN: focused dynamic/settings/dashboard regressions pass; SQLite restore saves the seven-day cap without moving the original time and a released circuit stays closed after restart; 11 web contract tests and 10 adjacent settings tests pass.
- Dynamic and dashboard full packages pass; runtime resource tests and settings release/authorization/interlock tests pass. Dynamic policy/release regressions also pass under `-race`. The new settings manager is exercised through its handler integration rather than an empty controller test selection.
- Web typecheck, changed-file ESLint and i18n checks pass. Vite/Playwright checks measured a 44px phone action, no horizontal overflow, and one remaining sibling after release. Product build/E2E were not run under the operator's no-build instruction. The temporary preview and overlay are removed after use.
- Full controller tests hit unrelated `TestNativeOpenCodeManualAPIRejectsUnverifiedFallback` (no stable versions) and `TestHostRuntimeUpdaterInvalidatesOnlyManagedNPMExecutionTree` (10-minute timeout). Full handlers hit two runtime-update job timeouts; both reproduced with the baseline `origin/main` handler route wiring via a temporary Go overlay. No runtime-updater changes were made.
- Specification catalog/lint and 63 public-doc validator tests pass. The full public-doc validator has a baseline-only missing coverage entry for `report_to_handoff_source_kandev`; baseline `coverage.json` has the same omission.
- The first normal commit hook run reported zero Go lint issues but exceeded its five-minute limit after cold Windows package loading. All other applicable hooks passed. Retry uses the repository's `-trimpath` cache convention for that command only; no hook is skipped or reconfigured.
- The next attempt encountered the shared linter's parallel-run lock. A process inventory later confirmed no active linter before retry; other lane processes and worktrees were not modified.
- UTC storage follow-up: a local KST failure time was saved as `13:47+09:00`, replacing the existing UTC storage convention. `TestCircuitPolicyPersistsFailureTimeInUTC` failed before the one-line normalization and passes with the same instant stored as `04:47+00:00`. This preserves chronological SQL comparisons without changing the original failure instant or cap deadline.
- Read-only production snapshot: all six legacy Go quota keys match the cap. Expected cap instants in KST are 10-14 17:33 (Qwen), 18:25 (Muse), 18:48 (MiMo), 18:59 (GLM Flash), 10-15 01:35 (DeepSeek), and 10-17 00:18 (GLM). These are policy-derived estimates, not executed production changes. Four OpenRouter generic-limit rows would initially use short retries; existing rows cannot be safely reclassified without their original cause.
