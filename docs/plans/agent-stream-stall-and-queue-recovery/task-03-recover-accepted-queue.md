---
id: "03-recover-accepted-queue"
title: "Recover queued work after an accepted prompt error"
status: done
wave: 2
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001
acceptance_criteria:
  - AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.1
  - AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.2
  - AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.3
  - AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.4
system_design:
  - ../../specs/tasks/system-design/queued-post-dispatch-recovery.md
---

# Task 03: Recover Queued Work After an Accepted Prompt Error

## Summary

The accepted row is already acknowledged on `acceptedPromptDispatchError`.
After that settlement, request a guarded successor evaluation so unrelated
queued work does not wait for backend restart.

## In scope

- Update `internal/orchestrator/event_handlers_agent.go` around
  `finishQueuedMessageExecution` and its caller's `err == nil` drain gate.
- Reuse the existing queue reservation, exact session-incarnation, primary
  transfer, and session recovery checks. Keep terminal recovery visible when
  no eligible recipient exists.
- Extend `queue_dispatch_recovery_test.go` or adjacent existing orchestrator
  tests with accepted-error/next-row, terminal/no-primary, eligible primary,
  Auto-run OFF, concurrent ready, and restart cases.

## Exclusions

- No replay of the accepted prompt, automatic enablement of Auto-run, workflow
  step completion, fresh process launch that bypasses recovery policy, or UI
  redesign.

## Implementation acceptance

1. The accepted source row is removed once and is never re-prompted.
2. The next row is evaluated promptly through normal gates after the failed
   turn settles; if blocked, it remains durable with a visible recovery path.
3. Concurrent and restart triggers cannot skip, reorder, or duplicate a row.

## Verification

From `apps/backend`, run:

```text
go test -p 2 -tags fts5 -run '^TestAcceptedQueuedPromptRecovery' ./internal/orchestrator
```

No listener-opening tests, full Go suite, or E2E.

## Dependencies and risks

This work order has no code dependency on Tasks 01/02. The accepted-error
case may leave the original session FAILED; a retry must not run under that
state merely because a queue row exists. Use the established eligible
primary/recovery path or retain the row and its recovery surface.

## Results

After an accepted post-dispatch error, the original row is acknowledged and
the dispatch marker is cleared before one identity-bound, admission-gated
successor evaluation. A failed/cancelled source transfers its remaining queue
only to a different live primary session already waiting for input. Otherwise
the queue remains durable on the failed source, with a bounded recovery
disposition log. Auto-run OFF, a concurrent ready signal, and restart do not
replay the accepted row or duplicate the successor.

From `apps/backend`: `go test -p 2 -tags fts5 -run '^TestAcceptedQueuedPromptRecovery' ./internal/orchestrator` exited 0. One trial exposed a test guard deadlock; the test was corrected to deliver the concurrent ready signal on a goroutine, then the exact command passed. No listener test was run.
