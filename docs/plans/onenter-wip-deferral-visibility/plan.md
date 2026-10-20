---
created: 2026-10-20
status: implemented
requirements:
  - REQ-TASKS-WIP-LIMIT-PULL-SYSTEM-001
system_design:
  - ../../specs/tasks/system-design/wip-limit-pull-system.md
legacy_specs: []
---

# Implementation Plan: Visible On-Enter Deferral for Full WIP Steps

## Overview

Make an engine-driven transition into a full WIP-limited step record why the
destination's entry behavior did not run. The transition, queue placement, and
promotion behavior stay unchanged.

## Confirmed root cause

- An agent's completion signal moved a task from a preparation step into an
  execution step declaring `on_enter: [auto_start_agent]` and `wip_limit: 1`.
  Another task already occupied that step.
- The admission write queued the task at the destination (`wip_admitted=0`,
  `queued_for_step_id` set). `applyEngineTransitionWithCommitMode` then
  returned before `launchProcessOnEnter`, which is the deferral that
  AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.3 requires.
- That return wrote no log entry. Operators saw `workflow transition applied`
  and the task state change, then nothing. That record looked the same as an
  on_enter action that had been lost.
- Being the new primary session after a recovery did not affect the outcome.
  The decision depends only on destination capacity.

## Scope

### In scope

- One `Info` log entry on the deferral return, naming task, session, source
  and destination steps, WIP limit, and the reason `wip_limit_reached`.
- A unit test that reproduces the queued engine transition and asserts the
  entry and the absence of on_enter processing.

### Out of scope

- Changing admission, promotion order, or session state for queued arrivals.
- User-interface changes. AC-001.8 already shows queued placement.
- Ledger-owned entry actions, which `Repository.dispatchStepEntry` runs on its
  own path.

## Work orders

- [completed] [Task 01: Log the on-enter deferral](task-01-log-onenter-deferral.md)

## Dependency order

```text
Task 01
```

## Verification strategy

- An in-process orchestrator test drives `processOnTurnCompleteViaEngine`
  against the SQLite repository with an occupant task. It asserts that the
  task is queued, that on_enter does not start, and that exactly one deferral
  entry carries the expected fields.
- Existing WIP, queue-promotion, and on_enter orchestrator tests continue to
  pass.

## Verification results

- The new test failed before the change (`on_enter deferral log count = 0,
  want 1`) and passed after it.
- The selected WIP, queue-promotion, and `TestProcessOnEnter_*` tests passed.

## Risks

- The entry fires once per committed engine transition into a full step, and
  never on retries or promotion. Its volume is bounded by transitions.
