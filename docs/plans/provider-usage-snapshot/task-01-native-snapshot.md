---
id: 01-native-snapshot
title: Native provider usage snapshot in agent settings
status: in_progress
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-COSTS-PROVIDER-USAGE-SNAPSHOT-001
acceptance_criteria:
  - AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.1
  - AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.2
  - AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.3
  - AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.4
  - AC-COSTS-PROVIDER-USAGE-SNAPSHOT-001.5
system_design:
  - ../../specs/costs/system-design/provider-usage-snapshot.md
---

# Native snapshot

Implement registry/config/profile/frontend flag wiring, snapshot DTO/controller/
handler, backend composition and account binding, Go client and ordinary agent
profile display. Preserve Office/hold/order/history. Exclude admission, learning,
statistics, cache-backoff changes, recurring polling and model probes.

## Acceptance

1. Enabled API/settings show shared-account provider readings and honest unknowns.
2. Disabled API/UI perform no new provider fetch; existing Office remains usable.
3. Only bounded source metadata leaves the backend and desktop/phone share UI-01.

## ASCII UI preview

UI-01 from [the plan](plan.md#ascii-ui-preview): existing name/model/fallback and
actions, followed by wrapping provider/account/class, window/state and a 44px
Refresh control. Loading/unavailable/no-API replace the percentage explicitly.

## Targeted verification

From `apps/backend`:

```text
go test -p 2 ./internal/runtimeflags ./internal/common/config ./internal/profiles
go test -p 2 ./internal/agent/usage ./internal/agent/settings/controller ./internal/agent/settings/handlers ./internal/backendapp -run 'TestUsageService|TestTeamClaude|TestCodexFetchUsage'
```

From `apps`, after `pnpm install --frozen-lockfile`:

```text
pnpm --filter @kandev/web test -- lib/state/slices/features/features-contract.test.ts
```

From `apps/web`: typecheck, changed-file lint, i18n checks and disposable rendered
desktop/phone/fake-transport proof. Run `git diff --check` before root handoff.
No production provider, task, credential or operating DB mutation.

## Results

Implementation is ready for integration review. Native defaults are false in
all three shipped profiles; no installation was enabled. The snapshot labels
`host_default` explicitly and does not claim to measure a later task/executor
override. The read cache uses the existing backend/auth/profile-version identity
fields directly, without importing the unrelated API barrel.

- Frozen pnpm installation completed successfully. Existing frontend feature
  contract and agent-profile-row tests passed: 2 files, 26 tests.
- Existing runtimeflags and profiles package tests passed. Existing UsageService,
  Codex client and TeamClaude tests passed. Controller/handler packages compiled;
  the initial existing-test filter selected no tests in those two packages.
- Disposable Go-overlay checks passed in usage, backendapp and handlers: missing
  credentials avoid HTTP, absent/negative Go measurements stay unknown, measured
  zero remains zero, reset parsing preserves uncertainty, 403 bodies stay private,
  same-account profiles share one fetch, free Go/Zen receive no paid windows,
  remote/custom bindings stay unknown, and disabled HTTP returns 404. The later
  binding-scope-only backendapp check passed as well.
- Disposable React checks passed 4 tests for disabled/no-fetch behavior, one
  shared request and explicit refresh, free/paid/source/scope display, login-scope
  late-response isolation and safe transport-error display. These exercise the
  real hook and component with a fake transport. No interval was introduced.
- TypeScript passed with a process-local 8 GiB heap after the default 4 GiB
  process exhausted its heap. No persistent runtime/compiler setting changed.
- Changed-file ESLint with `--max-warnings 0` passed. The i18n checks passed;
  localized host-scope copy subsequently used the existing catalog generators.
- The existing PR-docs `validateCoverage` accepted the actual changed paths and
  four linked document bodies: `covered`, `ok: true`, `errors: []`.
- Changed backendapp lint passed with zero issues after extracting two repeated
  literals. The initial wider lint run timed out at five minutes; the remaining
  six package checks subsequently completed with exit 0 and `0 issues.` through
  the serial-lock-respecting invocation below. The earlier timeout remains a
  failed attempt and is not counted as a passing run.

Completed lint invocation (backend working directory, process-local worker and
memory bounds only):

```text
golangci-lint run --allow-serial-runners ./internal/agent/usage ./internal/agent/settings/controller ./internal/agent/settings/handlers ./internal/agent/settings/dto ./internal/common/config ./internal/runtimeflags --new-from-rev=HEAD --timeout=5m --concurrency=2
```

The full common/config package has five Windows baseline failures. Replacing
this branch's config.go with the untouched base source through a disposable Go
overlay reproduced all five with exit 1:

```text
TestHomeConfigCannotRelocateItself
TestInvalidYAMLValueNamesSelectedFileAndKey
TestInvalidYAMLRangeNamesSettingAndFile
TestHomeConfigInvalidFirstDoesNotFallThrough
TestHomeConfigDefaultDotDirectory
```

The first four compare raw Windows paths against errors formatted with quoted,
escaped paths. The last sets HOME while discovery uses Windows UserHomeDir.
Only the feature bool was added to config.go; these source tests and discovery
logic were not changed.

A disposable Vite/browser layout fixture did not reach the usage API before its
30-second page-load/response timeout. No browser-layout or installed-runtime
acceptance is claimed. The temporary fixture was stopped; the valid React/DTO
evidence is retained here rather than expanding this increment into a browser
runner or operating change. Root integration owns final lint status, independent
review, publication and any later installed desktop/phone acceptance.
The disposable probe sources, overlays and baseline copy were removed after
inspection. No permanent tests, background runner or persistent heap setting
were added.

Independent review corrected two snapshot-only accuracy defects: profile
`OPENCODE_CONFIG_CONTENT` now prevents a host-default reading, and additive
provider observation metadata distinguishes absent/null/negative percentages
from measured zero and fallback resets from reported resets. Existing Office
values, wire format, shared cache and binding registration remain intact.
Disposable overlay checks reproduced the old snapshot reporting an unobserved
zero as `ok`; the corrected reader and profile override check passed. Codex,
Claude legacy/limits, TeamClaude and Go checks preserve measured zero and reset
uncertainty, and existing affected client/service tests passed. No permanent
test was added; these review probes remain outside test collection for root
readback and cleanup.

Review correction commands (from `apps/backend`; `PROBE` below denotes the
disposable `provider-usage-observation-20261015` directory):

```text
go test -p 2 -overlay PROBE/before-overlay.json ./internal/backendapp -run '^TestSnapshotRejectsUnobservedZero$' -count=1
go test -p 2 -overlay PROBE/overlay.json ./internal/agent/usage ./internal/backendapp -run 'TestObservation|TestSnapshotRejectsUnobservedZero|TestSnapshotPreservesObservedZeroWithoutInventingReset|TestSnapshotPreservesReportedReset|TestSnapshotProfileContentOverrideRemainsUnknown' -count=1
go test -p 2 ./internal/agent/usage -run 'TestUsageService|TestTeamClaude|TestCodexFetchUsage|TestCodexRefreshRejectsEmptyAccessToken|TestCodexHasSubscriptionCredentials|TestClaude' -count=1
```

The first exited 1 as expected with the original reader: unobserved utilization
became `State: ok`, `0%` and a fallback reset. Both corrected runs exited 0.
The exact observation tests were `TestObservationCodexPresence`,
`TestObservationClaudeLegacyAndLimits`, `TestObservationTeamClaudeAndGoZero`,
and `TestObservationDoesNotChangeOfficeWire`. The corrected snapshot tests in
the command all passed, including the root-provided profile override probe.
The existing PR-docs coverage function returned `covered`, `ok: true`,
`errors: []` for all eight changed paths and the four referenced document bodies;
`git diff --check` exited 0.

The same binding review found no uppercase normalization in validation, DTO
conversion or launch-environment assembly. Windows-only key folding now covers
both host-path and credential overrides; Unix comparisons keep their original
case. The following targeted probe first exited 1 against the pre-fold reader
(`codex_home` incorrectly accepted), then exited 0 against the correction. It
also checks mixed-case `OPENCODE_CONFIG_CONTENT`, `OPENAI_API_KEY`, and the local
Windows environment's case-insensitive lookup using a synthetic value only.

```text
go test -p 2 -overlay PROBE/envcase-before-overlay.json ./internal/backendapp -run '^TestSnapshotProfileEnvKeyUsesHostCaseRules$' -count=1
go test -p 2 -overlay PROBE/overlay.json ./internal/backendapp -run '^TestSnapshotProfileEnvKeyUsesHostCaseRules$' -count=1
```

Final changed-package lint exited 0 with `0 issues.`:

```text
golangci-lint run --allow-serial-runners ./internal/agent/usage ./internal/backendapp --new-from-rev=HEAD --timeout=5m --concurrency=2
```

Its first run reported only a repeated `windows` literal (`goconst`); extracting
that literal to a local constant resolved it. No broader consumer or full test
suite was repeated for this correction.
