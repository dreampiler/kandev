---
created: 2026-10-12
status: completed
requirements:
  - REQ-OFFICE-AUTOMATION-CONTINUITY-003
system_design:
  - ../../specs/office/system-design/automation-runs.md
legacy_specs: []
---

# Implementation Plan: Failed automation-run generated-task settlement

## Overview

When an automation run reaches a persisted terminal `failed`, its generated
hidden `automation_run` task is settled through the supported archive lifecycle
instead of being left open in `CREATED`, `SCHEDULING`, `IN_PROGRESS`, or
`REVIEW` forever. The run row, its `error_message`, and its transcript are
preserved, and the failure stays visible in the automations feed.

The run row and the generated task are settled by different owners, and only
the run row was settled before this change: `ReconcileOpenRuns`,
`StopRun`, `markDispatchFailed`, and the orchestrator turn-failure path all
wrote the run row without touching the task. This plan adds a guarded,
idempotent settlement at those boundaries plus one startup reconciliation.

## Scope

### In scope

- A guarded settlement in the automation service that archives the generated
  **hidden** `automation_run` task when, and only when, its run is persisted
  terminal `failed`.
- Invocation at the failure-settlement boundaries plus one startup
  reconciliation, so a crash between the run write and the task archive
  self-heals.
- A task-service archive that refuses a live session instead of stopping it.
- Owning requirements and system-design criteria, narrow Go unit tests, and the
  fork PR / review / merge.

### Out of scope

- Any change to successful-run behavior, admission, concurrency, run
  statuses/enums, API/WS payloads, or frontend copy.
- Managed-conversation delivery (`TaskModeManagedConversation`), its `uncertain`
  enum, and the `#234` two-origin count exclusions.
- Deleting tasks (archive only), touching the operating database or settings,
  turning off schedules/tray/processes, or any global or scheduled auto-archive.
- Correcting a run whose terminal status is itself inaccurate after a restart
  (a separate, separately evidenced lifecycle gap; see Risks).

## Technical approach

### TaskArchiver seam and guarded settlement

Add a `TaskArchiver` interface (`ArchiveTaskIfIdle`) to
`apps/backend/internal/automation/service.go`, mirroring the existing
`TaskDeleter`, and wire it in `internal/backendapp/services.go` /
`turn_adapters.go`. The adapter translates the task-service sentinels
(`ErrTaskAlreadyArchived`, `ErrTaskActiveSessions`, `ErrTaskNotFound`) into
automation sentinels so the settlement classifies without importing the task
service.

`settleFailedRunTask` (`internal/automation/failed_run_task_settlement.go`)
acts only when the run is persisted `failed` with a non-empty `task_id`,
requires the hidden `automation_run` origin through the existing
`taskOriginLookup` (fail-closed on a nil or unresolved lookup), skips a task
referenced by another run or the continuation pointer (`runTaskHasReferences`),
skips a task whose workspace is in use (`RunWorkspaceInUse`), then archives and
treats `ErrTaskAlreadyArchived` / `ErrTaskNotFound` / `ErrTaskActiveSessions` as
success. The run row is never modified.

### Non-blocking boundary invocation

Failure settlement can run under an orchestrator session lock, where stopping an
execution or publishing a task event would re-enter that lock. The service
therefore enqueues failed runs onto `failedSettleCh` and a dedicated worker
drains them (`startFailedRunTaskSettlementWorker`), rather than archiving
inline. A full queue drops the enqueue and leaves the task to startup
reconciliation.

### Startup reconciliation

`ReconcileFailedRunTasks` runs once at startup beside `ReconcileOpenRuns`. It
reads `ListUnarchivedFailedRuns` (persisted `failed` runs whose generated task
still exists and is not archived) and settles each. A read error returns the
error and touches no task, so a transient store problem is never mistaken for
"nothing to settle".

### Refuse live sessions

`ArchiveTaskIfIdle` (`internal/task/service/service_tasks.go`) reuses the
existing `ArchiveTask` body but refuses with `ErrTaskActiveSessions` when the
task still holds an active session, instead of stopping it. The same read
`ArchiveTask` uses to choose which sessions to stop is the read that decides the
refusal, so a reply that raced the failure keeps the task.

## Tests

- `AC-OFFICE-AUTOMATION-CONTINUITY-003.6` in
  `internal/automation/service_failed_run_task_settlement_test.go`: hidden
  failed task archived; succeeded, open/uncertain, visible, unreferenced-origin,
  referenced, continuation, and live-session tasks untouched; repeated
  settlement idempotent; active-session refusal handled; each enqueue boundary
  (task-keyed failure, stop, stale restart) enqueues; startup reconciliation
  archives open tasks and skips already-archived ones; a missing tasks table is
  a no-op; the failed-run read.
- `TestService_ArchiveTaskIfIdle` in
  `internal/task/service/service_archive_idle_test.go`: refuses a running
  session without archiving, archives when idle.

New tests go in new files because the neighboring service and automation files
already exceed or approach the backend effective-line limit.

## Work orders

- [x] [Task 01: Settle failed-run generated tasks](task-01-failed-run-task-settlement.md)

## Verification results

- `go test -trimpath -run 'TestSettleFailedRunTask|TestReconcileFailedRunTasks|TestGetLatestFailedRunByTask|TestMarkRunFailedByTaskID_EnqueuesSettlement|TestStopRun_EnqueuesSettlement|TestReconcileOpenRuns_ArchivesStaleHiddenTask' ./internal/automation/...`
  passed.
- `go test -trimpath -run 'TestService_ArchiveTaskIfIdle' ./internal/task/service/...`
  passed.
- `go vet -trimpath ./internal/automation/... ./internal/backendapp/... ./internal/task/service/...`
  passed.
- `gofmt -l` on every changed Go file reported no unformatted file.
- `python3 scripts/list-docs.py validate` and
  `python3 scripts/lint-spec-files.py --all` passed.

## Risks

- A user reply that starts a session between the liveness check and the archive
  could be stopped. The live-session refusal in `ArchiveTaskIfIdle` closes this:
  the archiver refuses rather than stops, and a residual refusal simply leaves
  the task for the next startup pass.
- An over-broad settlement could terminate visible `automation_task` work or a
  `reuse_thread` continuation; the origin and reference guards are mandatory and
  tested.
- A transient read error while listing failed runs must leave tasks untouched
  and be retried, never fail-open into archival.
- Silent failure-as-success is forbidden: the run stays `failed`; archiving only
  affects the task row and is recoverable.
- A distinct, separately evidenced gap remains out of scope for this change: a
  run whose exact turn completed before a restart can still be classified
  `failed` by `ReconcileOpenRuns`. That classification must be corrected from
  turn/run completion and terminal-event-persistence evidence rather than by
  archiving tasks, and it is handled as its own reviewable unit.
