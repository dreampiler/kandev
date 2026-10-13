---
id: "01-failed-run-task-settlement"
title: "Settle failed-run generated tasks"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-OFFICE-AUTOMATION-CONTINUITY-003
acceptance_criteria:
  - AC-OFFICE-AUTOMATION-CONTINUITY-003.6
system_design:
  - ../../specs/office/system-design/automation-runs.md
---

# Task 01: Settle Failed-Run Generated Tasks

## Summary

When a run reaches persisted terminal `failed`, archive the hidden
`automation_run` task it generated through a guarded, idempotent settlement, at
the failure boundaries and once at startup, so a failed firing never leaves
`CREATED`, `SCHEDULING`, `IN_PROGRESS`, or `REVIEW` work open. Preserve the run
row, its `error_message`, and its transcript.

## In scope

- Add the `TaskArchiver` seam and the guarded `settleFailedRunTask` helper.
- Invoke settlement at the failed boundaries of `ReconcileOpenRuns`, `StopRun`,
  `markDispatchFailed`, `MarkRunTerminal`, `MarkRunTerminalByBinding`, and
  `MarkRunFailedByTaskID`, through a non-blocking worker.
- Add `ReconcileFailedRunTasks` and its startup wiring in `provider.go`.
- Add the `ListUnarchivedFailedRuns` and `GetLatestFailedRunByTask` reads.
- Add `ArchiveTaskIfIdle`, which refuses rather than stops a live session.
- Wire the archiver adapter in `backendapp`.
- Add the narrow unit tests and the owning requirements / system-design criteria.

## Out of scope

- Successful-run behavior, admission, concurrency, run statuses, API/WS
  payloads, and frontend copy.
- Managed-conversation `uncertain` delivery and the `#234` two-origin count.
- Deleting tasks, writing the database directly, global or scheduled
  auto-archive, or re-archiving historical operating tasks.
- Correcting an inaccurate terminal run status after restart.

## Acceptance

- A persisted `failed` run with a hidden generated task archives that task; the
  run row, its error message, and its transcript are unchanged.
- A `succeeded`, open (`triggered` / `task_created`), uncertain-delivery, or
  visible-`automation_task` run, a referenced or continuation task, and a task
  with a `STARTING` / `RUNNING` session are left untouched.
- The settlement is idempotent: a repeated settlement of an already-archived
  task, or a missing task, is success, and startup reconciliation completes a
  settlement dropped between the run write and the archive.

## Verification

```bash
go test -trimpath -run 'TestSettleFailedRunTask|TestReconcileFailedRunTasks|TestGetLatestFailedRunByTask|TestMarkRunFailedByTaskID_EnqueuesSettlement|TestStopRun_EnqueuesSettlement|TestReconcileOpenRuns_ArchivesStaleHiddenTask' ./internal/automation/...
go test -trimpath -run 'TestService_ArchiveTaskIfIdle' ./internal/task/service/...
go vet -trimpath ./internal/automation/... ./internal/backendapp/... ./internal/task/service/...
```

Run every command from `apps/backend`. Run `gofmt -l` on every changed Go file.

## Files likely touched

- `apps/backend/internal/automation/failed_run_task_settlement.go`
- `apps/backend/internal/automation/service.go`
- `apps/backend/internal/automation/store.go`
- `apps/backend/internal/automation/provider.go`
- `apps/backend/internal/backendapp/turn_adapters.go`
- `apps/backend/internal/backendapp/services.go`
- `apps/backend/internal/task/service/service_tasks.go`
- `apps/backend/internal/automation/service_failed_run_task_settlement_test.go`
- `apps/backend/internal/task/service/service_archive_idle_test.go`
- `docs/specs/office/requirements/automation-continuity.md`
- `docs/specs/office/system-design/automation-runs.md`

## Dependencies

None.

## Risks

- A reply that starts a session during settlement must not be terminated; the
  archiver refuses on a live session.
- The origin and reference guards must not be bypassed, or visible or
  continuation work could be archived.

## Parallelism

`sequential`

## Inputs

- `AC-OFFICE-AUTOMATION-CONTINUITY-003.6`.
- `docs/specs/office/system-design/automation-runs.md` data model and failure
  behavior.
- The reported failed-run-open-task observation and the current `ReconcileOpenRuns`,
  `StopRun`, `markDispatchFailed`, and terminal-write implementations.

## Results

- Added the `TaskArchiver` seam and the guarded `settleFailedRunTask`
  settlement with hidden-origin, reference, and liveness guards, a non-blocking
  worker, and startup reconciliation.
- `ArchiveTaskIfIdle` refuses a live session with `ErrTaskActiveSessions`
  instead of stopping it.
- `go test -trimpath` automation settlement and `ArchiveTaskIfIdle` suites
  passed; `go vet` over the three changed packages passed; `gofmt -l` reported
  no unformatted file.
