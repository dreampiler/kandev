---
id: "01-shared-capacity"
title: "Include taskless Office runs in shared session capacity"
status: in-progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
  - REQ-OFFICE-TASKLESS-001
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-001.3
  - AC-AGENTS-SESSION-CEILING-001.6
  - AC-AGENTS-SESSION-CEILING-001.8
  - AC-AGENTS-SESSION-CEILING-001.13
  - AC-OFFICE-TASKLESS-001.8
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
  - ../../specs/office/system-design/taskless-run-sessions.md
legacy_specs: []
---

# Task 01: Shared taskless capacity

Implement the [plan](plan.md) using the existing orchestrator controller and
Office run queue. Connect preparation/running population, reservation transfer,
exact-attempt terminal notification and capacity-only restoration. Preserve
provider decisions, logical/concrete profile routing and historical runs.

Use existing Go checks and temporary overlays over the existing fake-runtime and
SQLite fixtures. Verify contention, count union, refusal without attempt creation,
same-run resume and late predecessor completion. Permanent tests, operating
settings, installation and Git publication are outside this implementation work
order. Grades, utility executions and manual override policy remain original
follow-up scope, rather than implied completed behavior.

## Implementation verification (2026-10-15)

- Existing focused `go test -trimpath` checks passed for orchestrator admission,
  union/rebind/rekey/handoff, release and live settings. Affected Office repository,
  service and scheduler routing/run-session checks passed. Existing taskless
  dynamic-profile and interrupted-attempt fake-runtime checks passed.
- Temporary overlays exercised 20 concurrent admissions for one remaining slot,
  task/run union and persisted reservation transfer; full/unknown/disabled retry;
  concrete refusal without attempt creation and resumption of the same run through
  the real Office scheduler/SQLite/launch path; late predecessor completion; and
  startup failure with successful versus uncertain Stop. All passed. The existing
  reconcile fixture lacks the unrelated task table and emits a checkout-reaper
  diagnostic; the asserted run-session/queue/runtime results passed.
- Integration review found that retaining a reservation after an uncertain Stop
  could outlive the later terminal row. Counting now transfers to the preparing
  row immediately after persistence. An affected-path overlay proved uncertain
  Stop retains one slot, then a later terminal CAS drives the existing release
  signal/sweep and the same waiting run launches immediately, without waiting for
  reservation expiry. The probe passed; no new timeout or retry driver was added.
- A routed overlay passed with 20 prior capacity deferrals: the run retained its
  capacity-only wait, routing disable did not clear it, and the same run resumed
  without falling into the provider-attempt limit. Provider candidate order was
  retained.
- Existing documentation catalog validation and specification lint passed.
  `pr-docs.cjs.validateCoverage` read the actual changed paths and linked document
  contents and returned `status: covered`, `errors: []`.
- An attempted changed-package Go lint was refused because another golangci-lint
  process held the shared lock. Integration owns the final staged lint/review.

### Executed commands and retained results

Working directory: `apps/backend`. These are completed checks, not a new checklist.
`OVERLAY` below replaces only the removed machine-local overlay JSON filename;
the executed flags, packages and run expressions are retained. The temporary
overlay, probes and helper files were removed after verification. Do not
recreate them merely because the temporary files no longer exist.

```text
go test -trimpath ./internal/orchestrator -run 'Test(Population|Admit|ConcurrentAdmissions|HandOff|Rebind|Rekey|ReleaseCeiling|ConfirmCeiling|PersistTaskSessionState_Releases|PersistStrictTaskSessionState_Releases|SessionCapacity)' -count=1
ok github.com/kandev/kandev/internal/orchestrator 16.793s

go test -trimpath ./internal/office/service ./internal/office/scheduler ./internal/backendapp -run 'Test(Taskless|ContinuationSummary_InterruptedAttempt|.*LaunchDeferred|.*DeferredCapacity|.*SessionCeiling)' -count=1
ok github.com/kandev/kandev/internal/office/service 0.865s
ok github.com/kandev/kandev/internal/office/scheduler 0.264s
ok github.com/kandev/kandev/internal/backendapp 1.697s

go test -trimpath ./internal/office/repository/sqlite ./internal/office/service ./internal/office/scheduler -run 'Test(.*RunSession|.*Routing|.*LaunchDeferred|.*DeferredCapacity|.*SessionCeiling|.*Park|.*ProviderCapacity)' -count=1 -timeout 90s
ok github.com/kandev/kandev/internal/office/repository/sqlite 0.447s
ok github.com/kandev/kandev/internal/office/service 0.506s
ok github.com/kandev/kandev/internal/office/scheduler 0.273s

go test -trimpath -overlay OVERLAY ./internal/backendapp ./internal/office/scheduler -run '^TestCapacityProbe' -count=1 -timeout 90s -v
--- PASS: TestCapacityProbeRealTasklessDeferralAndResume (0.20s)
--- PASS: TestCapacityProbeFailedCleanupRetainsOwnSlot (0.21s)
ok github.com/kandev/kandev/internal/backendapp 0.789s
--- PASS: TestCapacityProbeRoutedWaitPreservesProviderState (0.01s)
ok github.com/kandev/kandev/internal/office/scheduler 0.157s

go test -trimpath -overlay OVERLAY ./internal/orchestrator -run '^TestCapacityProbeReleaseSignal' -count=1 -timeout 90s -v
--- PASS: TestCapacityProbeReleaseSignalUsesExistingSweep (0.00s)
ok github.com/kandev/kandev/internal/orchestrator 0.453s

go test -trimpath -overlay OVERLAY ./internal/backendapp -run '^TestCapacityProbeUncertainStopThenTerminal' -count=1 -timeout 90s -v
--- PASS: TestCapacityProbeUncertainStopThenTerminalImmediatelyResumesWaitingRun (0.54s)
ok github.com/kandev/kandev/internal/backendapp 1.046s
```

The final command is the affected-path check after integration's reservation
transfer correction. The preceding valid admission, normal launch, duplicate
callback and routing evidence is reused; those paths were not changed by that
correction. Its raw diagnostics include the intentionally uncertain fake Stop
and the existing reconcile fixture's missing task-table checkout-reaper message,
not a failed run/session assertion. The earlier shared-admission overlay also
reported `PASS` for `TestCapacityProbeSharedAdmissionAndRetry` (orchestrator
package `0.121s`), covering 20 simultaneous requests, union and unknown/disabled
population behavior. No check was rerun for this documentation update.

No operating setting, model order, runtime installation, task/conversation history
or Git reference was changed by this work order. The temporary probes are not
included in the repository. Recheck only affected paths if implementation changes
or integration/review supplies new failure evidence.

### Root integration checks

The existing SQL guard passed. The required full race-enabled store conformance
run failed on this Windows host; it is not recorded as a pass. A subsequent
focused TestStoreConformance/sqlite3/storage/crud run reproduced the existing
storage fixture error: 	emporary artifact path must be an absolute path.
The unchanged base owner_actions.go uses /tmp/ paths for temporary artifacts,
which are not absolute Windows paths. This increment changes no persistence
catalog, schema or storage artifact validation. The collaboration lane captured
the full JSON result with the same storage CRUD/timestamp/conflict/transaction
and previous-stable upgrade fixture errors; unrelated fixture repair remains
outside this increment. No full conformance run was repeated after diagnosis.

The public operator guide now explains that preparing/running taskless Office
sessions share the ceiling and capacity waits retain the run and provider retry
budget. The existing public-docs validator passed for all 48 published pages.

Final changed-package lint passed with 0 issues in two serial-lock-respecting
invocations: orchestrator/Office root/backendapp, then Office repository/sqlite,
scheduler and service (--new-from-rev=HEAD --timeout=5m --concurrency=2).
The earlier lock refusal was not a lint pass. All changed Go packages are covered.
