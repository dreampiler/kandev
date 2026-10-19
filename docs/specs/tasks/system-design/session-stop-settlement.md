---
status: current
system: tasks
requirements:
  - REQ-TASKS-SESSION-STOP-SETTLEMENT-001
created: 2026-10-20
owners:
  - kandev
---

# Session Stop Settlement Design

## Boundary and mapping

| Criteria | Implementation boundary |
| -------- | ----------------------- |
| .1       | `orchestrator.Service.reapPromptUnreadyExecution` with the exact `executionTeardownClaim` |
| .2       | `orchestrator.Service.StopSession` and `settleExecutionLessSessionStop` |

## Completed teardown owner

`reapPromptUnreadyExecution` stops the execution only when it wins the forced
cleanup claim. When the claim is held by another path, it reads that exact
claim and continues only if `cleanupCompleted` is set, matching
`cleanupAgentExecutionWithReason`. An unfinished owner keeps the existing
"teardown is already owned" error.

## Execution-less stop

When `executor.Stop` returns `lifecycle.ErrNoExecutionForSession`,
`StopSession` asks `settleExecutionLessSessionStop`. It captures the open turn
first and then reads the session, so a launch that changes state in between
fails closed. Only `WAITING_FOR_INPUT`, `COMPLETED`, `FAILED`, and `CANCELLED`
settle, through `completeTurnForTaskSessionCheckedOwned` with the captured turn
ID, which refuses a different active turn. A missing session row settles with
nothing to close. Other states return the original error.

## Verification

Orchestrator tests cover a completed teardown owner during prompt-readiness
recovery, an execution-less stop of a waiting session, and execution-less stops
of created, starting, and running sessions.
