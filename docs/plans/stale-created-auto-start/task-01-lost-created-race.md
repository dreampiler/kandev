---
id: "01-lost-created-race"
title: "Treat a lost CREATED race as a no-op auto-start"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-STALE-CREATED-AUTOSTART-001
acceptance_criteria:
  - AC-TASKS-STALE-CREATED-AUTOSTART-001.1
  - AC-TASKS-STALE-CREATED-AUTOSTART-001.2
  - AC-TASKS-STALE-CREATED-AUTOSTART-001.3
  - AC-TASKS-STALE-CREATED-AUTOSTART-001.4
  - AC-TASKS-STALE-CREATED-AUTOSTART-001.5
  - AC-TASKS-STALE-CREATED-AUTOSTART-001.6
system_design:
  - ../../specs/tasks/system-design/stale-created-auto-start.md
legacy_specs: []
---

# Work order 01: Lost CREATED race

## Outcome

An auto-start launch that loses its CREATED race leaves the session and task
unfailed, starts no agent, and delivers the step prompt once only while the
task is still on the dispatching step.

## Changes

- `apps/backend/internal/orchestrator/executor/executor.go`: add
  `ErrSessionStateChangedBeforeStart`.
- `executor/executor_execute.go` and `event_handlers_streaming.go`: wrap the
  marker in the CREATED and WAITING_FOR_INPUT refusal of the guarded STARTING
  write.
- `task_operations.go`: `startCreatedSession` completes its turn and returns the
  marked error without `handleSessionLaunchFailure`.
- `event_handlers_workflow.go` and `auto_start_created_state_lost.go`:
  `launchCreatedAutoStartStepPrompt` routes a non-CREATED reload and a marked
  launch error to `continueAutoStartAfterCreatedStateLost`.
- `event_handlers_stale_created_autostart_test.go`: production-sequence tests.

## Acceptance criteria

- **AC-TASKS-STALE-CREATED-AUTOSTART-001.1:**
  `TestStaleCreatedAutoStart_LostRaceAfterStepMoveIsNotFailure` and
  `TestStaleCreatedAutoStart_LostRaceOnSameStepDeliversPromptOnce`.
- **AC-TASKS-STALE-CREATED-AUTOSTART-001.2:**
  `TestStaleCreatedAutoStart_ReplayAfterSessionWaitingDoesNotLaunch`.
- **AC-TASKS-STALE-CREATED-AUTOSTART-001.3:**
  `TestStaleCreatedAutoStart_LostRaceOnSameStepDeliversPromptOnce`.
- **AC-TASKS-STALE-CREATED-AUTOSTART-001.4:**
  `TestStaleCreatedAutoStart_LostRaceAfterStepMoveIsNotFailure` and
  `TestStaleCreatedAutoStart_ReplayAfterSessionWaitingDoesNotLaunch`.
- **AC-TASKS-STALE-CREATED-AUTOSTART-001.5:** All three tests assert no agent
  process start for the losing launch.
- **AC-TASKS-STALE-CREATED-AUTOSTART-001.6:** Existing
  `TestAutoStartCreatedLaunch_*` and `TestWorkflowAsyncStartFailure_*` tests.

## Validation

- `go test -tags fts5 ./internal/orchestrator/ -run 'TestStaleCreatedAutoStart|TestAutoStartCreatedLaunch|TestWorkflowAsyncStartFailure' -count=1`
- `go test -tags fts5 ./internal/orchestrator/executor/ -count=1`
- `python3 scripts/list-docs.py validate` and `python3 scripts/lint-spec-files.py --all`
