---
id: "04-launch-queue-refresh-sources"
title: "Refresh the launch queue only from its sources"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-BOUNDED-TASK-STATUS-DELIVERY-001
acceptance_criteria:
  - AC-PLATFORM-BOUNDED-TASK-STATUS-DELIVERY-001.15
  - AC-PLATFORM-BOUNDED-TASK-STATUS-DELIVERY-001.2
system_design:
  - ../../specs/platform/system-design/bounded-task-status-delivery.md
---

# Task 04: Refresh the launch queue only from its sources

## Summary

The task status summary projector re-reads the launch queue only for events
that can change it, plus a task whose queue has not been observed.

## In scope

1. Add tests in `task/statussummary` before the change, counting
   `LoadLaunchQueue` calls through the existing projector config:
   - After the queue is observed, `message.added`, `turn.started`, and
     activity events do not call the loader (baseline: one call each).
   - `task.updated`, `task.state_changed`, `task_session.state_changed`, and
     `message.queue.status_changed` call it and publish a changed queue.
   - A task state whose queue was never observed loads it on any event.
2. Add a closed `isLaunchQueueRefreshEvent` set next to the existing refresh
   classifiers and gate the loader call in `handleEvent` with it.
3. Keep the missing-task handling for queue-status events unchanged.

## Out of scope

The loader, ceiling observation, sweep cadence, deferral records, summary
schema, and frontend.

## Acceptance

- The non-refresh event test fails on the baseline and passes after the
  change; refresh-source tests pass on both.
- Existing queued-prompt and launch-queue projector tests still pass.

## Verification

```bash
(cd apps/backend && go test -trimpath ./internal/task/statussummary -run '^TestProjectorLaunchQueue' -count=1)
(cd apps/backend && go test -trimpath ./internal/task/statussummary -run '^Test(ProjectorQueueEvent|ProjectorRestoresQueuedPromptCount|EqualLaunchQueue|LaunchQueueSummaryFromTask|TaskStatusSummaryLaunchQueue)' -count=1)
(cd apps/backend && go build ./... && go vet ./internal/task/statussummary/...)
```

## Files likely touched

- `apps/backend/internal/task/statussummary/projector.go`
- `apps/backend/internal/task/statussummary/projector_events.go`
- `apps/backend/internal/task/statussummary/projector_launch_queue_test.go`

## Dependencies

None.

## Risks

A future subscribed event that changes the queue must join the closed set;
the set sits next to the other refresh classifiers to keep that visible.

## Results

Pending implementation.
