---
id: execution-grades-task-01
title: Shared grade settings and admission
status: in_progress
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-AGENTS-EXECUTION-GRADES-001
acceptance_criteria:
  - AC-AGENTS-EXECUTION-GRADES-001.1
  - AC-AGENTS-EXECUTION-GRADES-001.2
  - AC-AGENTS-EXECUTION-GRADES-001.3
  - AC-AGENTS-EXECUTION-GRADES-001.4
  - AC-AGENTS-EXECUTION-GRADES-001.5
system_design:
  - ../../specs/agents/system-design/execution-grades.md
legacy_specs: []
---

# Shared grade admission work order

Implement the linked design without operating mutations. Root owns this worktree,
integration and Git references. Preserve the original explicit grade/shared cap
requirement and do not replace it with task priority or model selection tiers.

Use the existing settings CAS and admission controller. Preserve omission,
logical routing identity, durable execution grade and exact-attempt cleanup.
Settings UI must distinguish compatibility automatic admission from the new
shared policy. Manual requests receive no implicit bypass in shared policy.

Use affected existing tests and temporary overlays outside normal collectors.
Run required persistence checks, diagnose existing Windows fixture failures
without weakening assertions, and record exact results. No permanent new tests,
pollers, gates or shared verifier. Publication and operation use existing owners.

## Backend increment

Use the native settings fields `policy` and `grade_limits`, plus
`execution_grade` on task and logical profile create/read/partial-update DTOs.
Persist `admitted_execution_grade` on the existing task-session and Office
run-session owners. Existing settings CAS, locked task updates, launch reservation
handoff, terminal proof and capacity-only retry paths retain ownership.

The first increment covers task and Office admission/population/release/resume.
Utility and review consumers remain the next backend increment under AC-001.4;
frontend settings and grade selection are owned by the root integrator. Existing
automatic policy and its explicit manual override remain compatibility behavior;
the explicitly enabled `all` policy grants no manual or MCP bypass. Installation
settings, model order and operating numeric limits are not changed.

## Backend results

The task/Office backend increment is implemented. Utility/review admission and
the root-owned frontend remain the original next work, so this work order is not
complete. All source changes are local; no installation setting or operating DB
was changed and no model was invoked.

Existing affected checks passed for session-capacity settings, profile dynamic
and partial updates, task field updates and session summaries, the native
admission/population/rebind/rekey/handoff paths, Office run-session persistence
and priority rebuild, and the bound dynamic taskless launcher. Disposable Go
overlays outside normal test collection exercised these exact probe families:

- `TestGradeProbeConcurrentManualAndOfficeShareOneSlot`: 24 concurrent requests
  compete for one remaining grade slot; exactly one enters, another grade stays
  eligible, and release restores the population.
- `TestGradeProbeOverallAndUnknownDoNotBypassManual` and
  `TestGradeProbeLogicalInheritanceSnapshotAndPersistenceFailure`: shared manual
  limits, honest unknowns, compatibility automatic policy, logical binding and
  task precedence, frozen held/active grades, and failed-write cleanup.
- `TestGradeProbeTaskAndSessionPersistAndReplay` and
  `TestGradeProbeStaleTaskWriterPreservesExplicitGrade`: real SQLite omission,
  explicit inheritance, metadata replacement, replay, summary/population and
  terminal release. The stale-writer case first failed by restoring `initial`
  over `edited`, then passed after using the existing transaction's saved grade.
- `TestGradeProbeOfficeUsesLogicalGradeAndSharesTaskPopulation` and
  `TestGradeProbeOfficeResumeSkipsFullGrade`: the real Office launcher refuses
  without allocating an attempt/runtime when a task fills the grade, launches
  after that task settles, preserves the logical grade across a candidate and
  later edit, and restores an eligible grade behind a full one.
- `TestGradeProbeSettingsOmissionValidationAndLegacy` and
  `TestGradeProbeConcreteAndDynamicOmissionPreserveGrade`: saved settings and
  partial profile writes retain omission; zero remains an explicit cap;
  null/noninteger maxima are rejected; profile clear preserves routes/version.
  An older unrelated profile writer first overwrote the edited grade; shared SQL
  now writes that column only for an explicit partial grade intent and returns
  the committed value.
- `TestGradeProbeNestedCreateAndDuplicatePreserveGrade`,
  `TestGradeProbeBootCarriesGradeAndExplicitClear`, and
  `TestGradeProbeTaskUpdatedCarriesGradeAndExplicitClear`: existing nested
  profile creation, clone, boot whitelist and native task.updated carry both
  named grades and explicit clears. No endpoint or separate fetch was added.
- `TestGradeProbeExistingDestinationKeepsItsOwnGrade` first reproduced a new
  frozen-grade write refusing an already admitted destination. The transfer now
  returns only its own source reservation under the same mutex and keeps that
  destination's existing grade and population. The corrected test and existing
  rebind/rekey/handoff cases passed with the race detector.

Representative commands from `apps/backend` (`PROBE` denotes the disposable
execution-grade probe directory):

```text
go test -p 2 -overlay PROBE/overlay.json ./internal/task/repository/sqlite ./internal/backendapp ./internal/orchestrator ./internal/system/sessioncapacity ./internal/agent/settings/controller -run '^TestGradeProbe' -count=1
go test -p 2 -overlay PROBE/overlay.json ./internal/agent/settings/controller ./internal/backendapp ./internal/task/service -run '^TestGradeProbe|TestCreateAgentProfiles|TestCreateAgent_RejectsBadNestedProfileBeforeAnyWrite|TestMapKanbanTaskStateIncludesPriority|TestPublishTaskUpdated_EmitsAutopilot' -count=1
go run ./cmd/sqlguard ./internal
go test -race -p 2 -overlay PROBE/overlay.json ./internal/orchestrator -run '^TestGradeProbe|^TestRebind|^TestRekey|^TestHandOff' -count=1
```

The stable affected runs and sqlguard exited 0. The first all-package changed-Go
lint reported only setter cognitive complexity, retry nesting and an uppercase
error string; extracting helpers and correcting the string resolved them. Later
changed-package lint runs exited 0 with `0 issues.`; only changed packages were
rechecked after subsequent narrow corrections.

The required full `go test -race -p 2 ./internal/persistence/storeconformance
-count=1` exited 1. Its migration output exceeded the tool output limit, so a
bounded JSON diagnostic retained the original output and selected affected task,
agent-settings and Office owners plus the stable upgrade. Their fresh, replay,
CRUD, boolean, timestamp, conflict and supported transaction checks passed. The
upgrade reached its conformance scenarios and failed only there on
`storage_temp_artifacts`: `temporary artifact path must be an absolute path`
(`upgrade_test.go:192`), matching the independently confirmed existing Windows
`/tmp` fixture limitation. This does not claim a green full suite. The subsequent
agent-settings-only race conformance passed after the partial-write correction.
No PostgreSQL execution was available because `KANDEV_TEST_POSTGRES_DSN` was unset.

```text
go test -race -p 2 -json ./internal/persistence/storeconformance -run '^(TestUpgradeFixtureManifest|TestPreviousStableUpgrade|TestStoreConformance)$/^sqlite3$/^(task|office|agent-settings|v0[.]93[.]0)$' -count=1
go test -race -p 2 -json ./internal/persistence/storeconformance -run '^TestStoreConformance$/^sqlite3$/^agent-settings$' -count=1
```

The first diagnostic exited 1 on the stated storage upgrade fixture; the second
exited 0. Their JSONL outputs remain in the disposable location for root readback.

The existing explicit-tag manifest now checks empty legacy grade defaults after
upgrade. Historical fixture SQL and its existing checksum values were unchanged.
Existing settings assertions were adapted for the map fields and native defaults;
no permanent test function, poller, gate or new verifier was added.

`python scripts/lint-spec-files.py --all` and `python scripts/list-docs.py validate`
passed. The PR-docs coverage function accepted the actual changed paths and four
linked document bodies as `covered`, `ok: true`, `errors: []`.

## Native frontend increment and root verification

The native System editor uses the existing Save coordinator and settings CAS
for automatic/all policy and grade maxima, including explicit zero. Concrete
and dynamic profile editors and task create/edit flows share the grade field.
Unchanged edits omit grade writes; explicit empty writes clear the profile grade
or restore task inheritance. Boot, HTTP, task.updated, board/sidebar/mobile edit
sources and post-save hydration preserve named grades and explicit clears.
Legacy thin events that omit the field do not clear it. No runtime value,
profile order or operating installation was changed.

The existing 15 affected frontend files ran 246 tests: 245 passed and one exact
normalizer assertion failed because the new normalizer added an empty grade to
an omitted legacy field. Preserving omission corrected it. The three affected
normalizer/setup/submit files then passed all 71 tests; unchanged successful
files were not rerun. Full TypeScript compilation passed with a process-local
8 GiB Node heap. Changed frontend ESLint passed without warnings, after
extracting the new task grade initialization/validation and partial-patch
responsibilities rather than suppressing file/function/complexity limits.

Existing System settings API/editor checks passed 22 tests and the later
notice-extraction delta passed nine affected tests. Four disposable System
editor cases also passed. Existing i18n checks passed all six real locales,
pseudo keys, Trans/plural/module-scope and non-JSX string checks (12707 English
keys; the existing 507 orphan warnings remain). Generated unrelated pseudo
values were restored to their prior values.

Disposable copies of existing fixtures under the already excluded
node_modules/.cache location exercised the real save boundaries with only
GradeProbe selected. Three concrete-profile saves and three actual task edit
submits passed for unchanged, trimmed and explicit-empty grades. The dynamic
editor/coordinator case passed for a newer grade edit during an in-flight save,
discard to the committed grade and explicit clear. The initial temporary config
had an incorrect relative Vite config path; correcting that fixture path enabled
collection. Its first dynamic case called a nonexistent fixture discardAll
method; using the actual editor discardDraft completed that proof. Neither was
a product defect or passing result. No permanent test or verifier was added.

Commands from apps/web:

    pnpm exec vitest run [the 15 affected existing files]
    pnpm exec vitest run lib/api/domains/agent-profile-normalize.test.ts components/task-create-dialog-setup.test.ts components/task-create-dialog-submit.test.tsx
    pnpm exec vitest run --config node_modules/.cache/execution-grade-probe-20261015/vitest.config.ts -t GradeProbe
    pnpm exec vitest run --config node_modules/.cache/execution-grade-probe-20261015/vitest.config.ts dynamic.test.tsx -t GradeProbe
    pnpm exec tsc --noEmit
    pnpm exec eslint [changed frontend files] --max-warnings 0
    pnpm run i18n:check

The temporary probes establish React/state/API-boundary behavior with mocked
transport. Public usage documentation names the current task/Office scope and
the required remaining utility/review integration. This increment has no
installed-system or real-provider acceptance claim.

Root additionally used actual Chrome with the real native
SessionCapacitySettings, SettingsSaveProvider and shared ExecutionGradeField
under synthetic HTTP. At desktop 1440x1200, saving a zero grade limit produced
the exact native PATCH and a subsequent reload preserved it. At phone 390x844,
grade input and Remove controls measured 44px, there was no horizontal overflow,
and profile validation plus empty task inheritance worked. Page errors and
external requests were both zero. Root opened both result JSON files and all
four PNGs: desktop, phone top, scrolled grade rows and scrolled shared fields.
The synthetic preview identifies its fixture scope. It does not claim complete
task/profile-page browser or installed/backend/provider acceptance.

The first browser fixture selected Save while the actual native button is Save
changes; only that selector was corrected. Its phone full-page capture showed
the top of the native internal scroller, so two explicit scroll captures
supplemented it without rerunning the passing save/backend checks. Both Vite
server handles were closed and port37444 had no listener. The disposable React
fixture's four files were recycled after readback; browser/probe evidence is
retained only until integration review. Product source was unchanged by capture.

## Explicit workflow target admission correction

Independent review found one current admission defect: a sessionless start
reserved the caller/default profile's grade before the native SessionTarget
resolver chose its actual logical recipient. An actual LaunchSession/SQLite/
fake-launcher probe first launched the review profile despite its explicit zero
cap. It also showed that changing the source profile after grade lookup could
launch a different zero-capped logical profile. Existing explicit caller and
task-grade override precedence already passed and remain unchanged.

For `all`, admission and later route preparation now share one read-only native
selection frame, including the logical profile, reusable session and recorded
route. Route persistence, nonterminal promotion, creation and existing route CAS
remain at their original mutation boundaries. Automatic policy keeps its native
selection path. No model candidate selection or profile order was changed.

A selected destination that is already counted receives a distinct launch-owned
reservation alias unioned with that row. Its frozen admitted grade is retained,
and an unrelated launch's reservation is neither borrowed nor released. If the
destination loses its nonterminal promotion CAS, fresh fallback repeats the
ordinary atomic admission before allocating a new session; a zero-cap refusal
uses the existing durable queue and replay path.

The original four-case temporary probe returned two failures and two passes
(package 0.438s). After the first correction, the available-target snapshot,
explicit caller and task override passed (0.996s); the zero case had already
stopped before runtime/session creation, but its assertion incorrectly expected
an error instead of the native public queued response. Correcting only that
fixture assertion gave the narrowed zero case PASS (0.461s), retaining its
checks for queued response, no runtime, no session, deferred intent and no route
write. No product response contract was changed.

The two additional source-change/active-target cases first failed (0.464s).
The final three-case selection run passed (0.279s): source mutation keeps the
admitted logical recipient; a recorded running target at full shared capacity
and zero grade maxima reuses its existing row/frozen grade; and terminal CAS
loss queues fresh fallback before creation. An intermediate active-target
fixture omitted the recorded route's destination ID and reached the pre-existing
route promotion fence. Supplying the native prepared/committed route fixture
fixed that input; the production CAS was not relaxed.

Exact final commands from `apps/backend`, with `PROBE` denoting the existing
disposable execution-grade directory outside normal collectors:

```text
go test -trimpath -p 2 -overlay PROBE/overlay.json ./internal/orchestrator -run '^TestGradeProbeExplicitTargetAdmissionUsesFinalLogicalProfile$/(source_changes|active_target)' -count=1 -timeout 90s -v
go test -trimpath -p 2 ./internal/orchestrator -run '^Test(LaunchSession_ProfileExplicitBypasses(WorkflowStepOverride|SessionTargetStepOverride)|SelectExplicitWorkflowStartSessionFallsBackFromTerminalTarget|PreviewWorkflowSessionTargetUsesTaskWorkflowOverride|WorkflowSessionTargetUsesTaskEffectiveSourceProfile|AdmitOrDeferSeam1.*|Seam1Reservation.*|LoadRecordedWorkflowSessionRouteReturnsCommittedDestination|LoadRecordedWorkflowSessionRouteAllowsFreshFallbackFromTerminalDestination)$' -count=1 -timeout 90s -v
```

The second command passed (0.987s), covering the affected existing compatibility,
reservation ownership and route contracts. Raw final outputs remain in
`route-grade-selection-final.txt` and `route-grade-existing-final.txt` in `PROBE`
for root readback. Reuse these valid results unless the corresponding source or
fixture inputs change. Utility/review integration remains the original next
backend increment; this correction does not claim to complete it.

The correction's delta lint command was
`golangci-lint run ./internal/orchestrator --new-from-rev=HEAD --timeout=5m --concurrency=2`.
Its first run exited 1 with only `taskExecutionGrade` cyclomatic complexity
20 (maximum 15). Moving its unchanged final logical-profile routing/fallback
into `logicalTaskExecutionGrade` resolved that style issue. The same package's
final run naturally exited 0 with `0 issues.`; raw outputs are
`route-grade-lint.txt` and `route-grade-lint-final.txt` in `PROBE`. Existing
successful behavior checks were retained after this mechanical extraction;
no broader suite or permanent test was added. The final `git diff --check`
also exited 0. Source and probe inputs are returned to the root integrator.


The first normal staged commit compared against the upstream lint base and
reported two nestif excesses in countedRowsLocked, even though the narrower
delta lint against the capacity parent had passed. The same task and Office
projection logic was moved without behavioral changes into two owner-specific
helpers; no threshold or suppression was changed. All web formatting, lint and
i18n stages in that attempt passed. Normal commit hooks remain required after
this correction; the failed Go lint attempt is not recorded as a pass.
