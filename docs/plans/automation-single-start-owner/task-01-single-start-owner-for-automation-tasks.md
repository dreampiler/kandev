---
id: "01-single-start-owner-for-automation-tasks"
title: "Single start owner for automation tasks"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-001.10
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Task 01: Single start owner for automation tasks

## Summary

Make the automation dispatch the only initial starter of a task the automation
creates, so a feeder pull's workflow auto-start cannot race it.

## In scope

- Add regression tests for the owned-task skip, the create-time marker, and the
  marker's release before the change.
- Stamp `automation_start_owned` on a newly created automation task.
- Skip workflow auto-start for a task that carries the marker.
- Remove the marker when the automation dispatch resolves, publish
  `task.updated`, and clear abandoned markers at startup.
- Accept the marker only from the automation create path, never from request
  metadata or an ordinary metadata update.
- Record the single start owner in the session ceiling design.

## Out of scope

Continuation runs, the ceiling deferral comparison, automation failure cleanup,
and UI changes.

## Acceptance

- An owned task on an auto-start step starts no workflow launch; an unmarked
  task on the same step launches.
- A task the automation creates carries the marker when `CreateTask` runs.
- After the automation dispatch resolves, even with a cancelled context, the
  task no longer carries the marker and a `task.updated` is published.
- Startup reconciliation clears a marker left by a stopped process.
- Create request metadata and ordinary metadata updates cannot add or remove
  the marker.
- Existing deferred automation start tests pass.

## Verification

From the repository root:

```bash
(cd apps/backend && go test -trimpath ./internal/orchestrator -run 'Automation|AutoStart|TaskMoved|Feeder|WIP|Ceiling' -count=1)
(cd apps/backend && go test -trimpath ./internal/task/models ./internal/task/service -count=1)
(cd apps/backend && go vet ./internal/orchestrator ./internal/task/...)
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

## Files likely touched

- apps/backend/internal/orchestrator/event_handlers_automation.go
- apps/backend/internal/orchestrator/event_handlers_workflow.go
- apps/backend/internal/orchestrator/event_handlers_github.go
- apps/backend/internal/orchestrator/service.go
- apps/backend/internal/backendapp/orchestrator.go
- apps/backend/internal/task/service/service_requests.go
- apps/backend/internal/task/service/service_task_metadata.go
- apps/backend/internal/task/service/service_tasks.go
- apps/backend/internal/task/models/task_metadata_update.go
- apps/backend/internal/task/repository/sqlite/task.go
- apps/backend/internal/task/repository/sqlite/task_metadata_merge.go
- apps/backend/internal/orchestrator/event_handlers_workflow_automation_owned_start_test.go
- apps/backend/internal/orchestrator/automation_ceiling_defer_test.go
- apps/backend/internal/task/models/models.go
- apps/backend/internal/task/service/service_step_handoff_metadata_test.go
- docs/specs/agents/system-design/session-concurrency-ceiling.md

## Dependencies

None.

## Risks

A move of an automation task before its dispatch resolves does not auto-start
it; the automation dispatch, or its ceiling replay, starts it instead.

## Parallelism

`sequential`

## Inputs

- Issue #4323.
- REQ-AGENTS-SESSION-CEILING-001 and AC-AGENTS-SESSION-CEILING-001.10.
- The linked session ceiling design.

## Results

- RED: on the original code, the owned task started one workflow launch, the
  created automation task had no marker, and the marker was not released.
- GREEN: the new tests and the existing deferred automation start tests pass.
