---
id: "01-session-stop-settlement"
title: "Settle stops with nothing left to stop"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-SESSION-STOP-SETTLEMENT-001
acceptance_criteria:
  - AC-TASKS-SESSION-STOP-SETTLEMENT-001.1
  - AC-TASKS-SESSION-STOP-SETTLEMENT-001.2
system_design:
  - ../../specs/tasks/system-design/session-stop-settlement.md
---

# Task 01: Settle stops with nothing left to stop

## Summary

Continue a prompt-readiness resume when the execution's teardown is already
complete, and settle an execution-less stop of a waiting or terminal session by
closing its observed open turn.

## In scope

- `reapPromptUnreadyExecution` accepts a completed exact teardown claim; the
  stop itself moves to `stopPromptUnreadyExecution`.
- `StopSession` calls `settleExecutionLessSessionStop` on
  `lifecycle.ErrNoExecutionForSession`.

## Out of scope

Live execution stops, cleanup arbitration, `StopSessionSynchronously`, and
background work settlement.

## Verification

- `go test ./internal/orchestrator/ -run 'TestReapPromptUnreadyExecution|TestStopSession'`
