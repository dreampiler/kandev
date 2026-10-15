---
id: "01-requeue-expired-runtime-wait"
title: "Keep queued messages after an expired runtime wait"
status: done
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
- Match the ensure-session classifier prefix and wrapped deadline in the
  retryable condition in `handleQueuedMessageExecutionError`.

## Out of scope

- Changing `ErrSessionRuntimeUnavailable` or the ensure-session classification.
- Any timer, retry loop, or new resume attempt.

## Acceptance

- A reserved entry that fails with the classified runtime-wait deadline is the
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

- RED before the fix: `TestQueuedMessageExecutionError_RequeuesExpiredRuntimeWait`
  failed with "expired runtime wait left 0 queue entries".
- GREEN after the fix: both new tests and
  `TestQueuedMessageExecutionError_RequeuesSeam3Refusal` pass.
- The broader `-run 'Queued|Queue|RuntimeUnavailable|Deadline'` group passes
  except `TestHandleAgentReady_PassthroughQueuedAttachmentUsesAttachmentAwarePrompt`,
  which also fails without this change on Windows because the attachment path
  uses `\` separators.
