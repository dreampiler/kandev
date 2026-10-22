---
created: 2026-10-20
status: done
requirements:
  - REQ-TASKS-RESUME-PROMPT-QUEUE-003
system_design:
  - ../../specs/tasks/system-design/resume-prompt-queue.md
legacy_specs: []
---

# Implementation Plan: Keep queued work when shutdown cancels its resume

## Overview

During a graceful shutdown `Service.Stop` cancels in-flight resume attempts. A
queued dispatch waiting on one fails with `ErrResumeAttemptCancelled`, which
`finishQueuedMessageExecution` and `handleQueuedMessageExecutionError` did not
recognize. The entry fell into the dropped branch, which neither acknowledged
nor restored it and logged "message is lost". The dispatch claim stayed, and
the next boot parked it as `unknown_queue_dispatch_outcome`, needing a manual
`session.recover`. This breaks AC-TASKS-RESUME-PROMPT-QUEUE-001.7.

## Scope

In scope: `apps/backend/internal/orchestrator/event_handlers_agent.go`
(`finishQueuedMessageExecution`, the dropped-branch log in
`handleQueuedMessageExecutionError`), a permanent orchestrator test, and the
`resume-prompt-queue` requirement and design amendments.

Excluded: the shutdown order in `Service.Stop`, the resume attempt registry,
the startup queue dispatch reconciler, and any new retry or timer.

## Contract changes

`resume-prompt-queue` gains requirement 003. The exclusion of an automatic
retry of a failed resume still holds: the entry waits for the next existing
trigger.

## Technical approach

- `finishQueuedMessageExecution` routes `ErrResumeAttemptCancelled` without a
  delivery attempt to the same restore as the recovery-required branch:
  `restoreQueuedMessageForSession` for an exact reservation, otherwise
  `restoreQueuedMessage`.
- The dropped-branch warning says the message was dropped and that no retry
  applies, instead of "message is lost".

## Work orders

- [x] [Task 01: Restore queued messages after a cancelled resume](task-01-restore-after-cancelled-resume.md)

## Changed-path coverage

The work order owns `event_handlers_agent.go`, the new
`queue_cancelled_resume_restore_test.go`, and the `resume-prompt-queue`
requirement and system-design amendments.

## Risks

The restore runs on the reservation's recovery owner. When `Stop` stops that
owner before the restore is launched, the claim stays for the existing startup
reconciliation, as before this change.
