---
id: "01-restore-after-cancelled-resume"
title: "Restore queued messages after a cancelled resume"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-RESUME-PROMPT-QUEUE-003
acceptance_criteria:
  - AC-TASKS-RESUME-PROMPT-QUEUE-003.1
  - AC-TASKS-RESUME-PROMPT-QUEUE-003.2
  - AC-TASKS-RESUME-PROMPT-QUEUE-003.3
system_design:
  - ../../specs/tasks/system-design/resume-prompt-queue.md
---

# Task 01: Restore queued messages after a cancelled resume

## Summary

A reserved queued entry whose dispatch fails with `ErrResumeAttemptCancelled`
before any delivery attempt returns to its session queue and its dispatch
claim is settled.

## In scope

- Write the failing orchestrator test first, for an ordinary and an exact
  reservation.
- Restore in `finishQueuedMessageExecution` beside the recovery-required
  branch.
- Reword the dropped-branch warning.

## Out of scope

- The shutdown order, the resume attempt registry, and the startup reconciler.
- Any timer, retry loop, or new resume attempt.

## Verification

- `go test ./internal/orchestrator/ -run '^TestCancelledResumeRestores' -count=1`
  (real SQLite queue in a temp directory, no process is started)
