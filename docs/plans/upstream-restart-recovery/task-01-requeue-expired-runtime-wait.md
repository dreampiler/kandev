---
id: "01-requeue-expired-runtime-wait"
title: "Keep queued messages after an expired runtime wait"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-RESUME-PROMPT-QUEUE-002
acceptance_criteria:
  - AC-TASKS-RESUME-PROMPT-QUEUE-002.1
  - AC-TASKS-RESUME-PROMPT-QUEUE-002.2
  - AC-TASKS-RESUME-PROMPT-QUEUE-002.3
system_design:
  - ../../specs/tasks/system-design/resume-prompt-queue.md
---

# Task 01: Keep queued messages after an expired runtime wait

## Summary

A reserved queued message whose dispatch fails with
`failed to ensure session is running: context deadline exceeded` returns to
the head of its queue instead of being dropped.

## In scope

- Write the failing orchestrator test first.
- Add `errors.Is(err, context.DeadlineExceeded)` to the retryable condition in
  `handleQueuedMessageExecutionError`, with a comment that names the expired
  runtime wait.

## Out of scope

- Changing `ErrSessionRuntimeUnavailable` or the ensure-session classification.
- Any timer, retry loop, or new resume attempt.

## Acceptance

- A reserved entry that fails with a wrapped `context.DeadlineExceeded` is the
  only, unreserved, original entry in its session queue afterwards.
- A non-retryable error still takes the lost branch.

## Verification

From the repository root:

```bash
(cd apps/backend && go test -trimpath -tags fts5 ./internal/orchestrator -run 'TestQueuedMessageExecutionError' -count=1)
```

## Likely files

- `apps/backend/internal/orchestrator/event_handlers_agent.go`
- A source-adjacent `*_test.go` in `apps/backend/internal/orchestrator`

## Results

Not implemented yet. On the current main branch, a probe with this fixture
leaves zero queue entries after the failure.
