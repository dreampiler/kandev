---
id: "01-log-onenter-deferral"
title: "Log the on-enter deferral"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WIP-LIMIT-PULL-SYSTEM-001
acceptance_criteria:
  - AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.3
  - AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.10
system_design:
  - ../../specs/tasks/system-design/wip-limit-pull-system.md
---

# Task 01: Log the On-Enter Deferral

## Summary

Record one operator log entry when an engine-driven transition into a full
WIP-limited step defers the destination's entry behavior until promotion.

## In scope

- Add a failing orchestrator test. In it, a completion transition enters a
  full `wip_limit: 1` step that declares `auto_start_agent`.
- Write the `Info` entry on the queued-deferral return of
  `applyEngineTransitionWithCommitMode`.

## Out of scope

- Admission, promotion, or session-state changes.

## Acceptance

- The task is queued at the destination and on_enter processing does not
  start.
- Exactly one entry `workflow on_enter deferred until destination step admits
  the task` names `task_id`, `session_id`, `from_step_id`, `to_step_id`,
  `wip_limit`, and `reason=wip_limit_reached`.

## Validation

```bash
cd apps/backend
go test -trimpath -count=1 -run 'TestEngineTransition_WIPFullDestination|TestQueuePromotion|TestProcessOnEnter_' ./internal/orchestrator/
```
