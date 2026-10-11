---
id: "03-slow-poll-idle-backoff"
title: "Back off unchanged slow-mode polling"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-WORKSPACES-LOCAL-REPOSITORIES-004
acceptance_criteria:
  - AC-WORKSPACES-LOCAL-REPOSITORIES-004.6
  - AC-WORKSPACES-LOCAL-REPOSITORIES-004.4
system_design:
  - ../../specs/workspaces/system-design/local-repositories.md
---

# Task 03: Back off unchanged slow-mode polling

## Summary

In slow mode, each polling loop lengthens its interval while its completed
scans observe no change, capped at 5 minutes. A change or any mode push
returns to 30 seconds. Fast and paused modes are unchanged.

## In scope

1. Add tests in `agentctl/server/process` before the change, driving the
   existing tick functions against a real temporary repository:
   - After repeated unchanged slow-mode monitor ticks, the slow file interval
     from `pollIntervals` follows 30, 30, 30, 60, 120, 240, 300 seconds
     (baseline: always 30).
   - The Git poll loop backs off on its own counter.
   - A changed tick resets its loop; `SetPollMode`, including a same-mode
     push, resets both loops and wakes them to re-arm a lengthened timer.
   - Failed ticks do not advance; fast and paused intervals ignore backoff.
2. Add one idle counter per loop on `WorkspaceTracker`. Advance it from the
   completed-scan outcome in `monitorTick` and `gitPollTick`, where
   `checkGitChanges` reports whether any tracked value moved.
3. Apply the schedule inside `pollIntervals` for slow mode only, so the loops
   keep one interval source. Reset both counters in `SetPollMode` before the
   same-mode return, and wake the loops when a lengthened interval was reset.

## Out of scope

Fast and paused intervals, the startup grace, mode aggregation, focus wakeups,
turn-end final scans, and access-denied pausing.

## Acceptance

- The schedule tests fail on the baseline and pass after the change.
- Existing poll mode, loop, and monitor tests still pass.

## Verification

```bash
(cd apps/backend && go test -trimpath ./internal/agentctl/server/process -run '^TestSlowPoll' -count=1)
(cd apps/backend && go test -trimpath ./internal/agentctl/server/process -run '^Test(PollMode_IsValid|NewWorkspaceTracker_DefaultsToFast|SetPollMode_|PollIntervals|MonitorLoop_)' -count=1)
(cd apps/backend && go build ./... && go vet ./internal/agentctl/server/process/...)
```

## Files likely touched

- `apps/backend/internal/agentctl/server/process/workspace_poll_mode.go`
- `apps/backend/internal/agentctl/server/process/workspace_monitor.go`
- `apps/backend/internal/agentctl/server/process/workspace_git_poll.go`
- `apps/backend/internal/agentctl/server/process/workspace_tracker.go`
- `apps/backend/internal/agentctl/server/process/workspace_poll_backoff_test.go`

## Dependencies

None.

## Risks

An unfocused badge can lag a change by up to 5 minutes after a long quiet
period. This is the intended cadence trade-off recorded in the design.

## Results

Pending implementation.
