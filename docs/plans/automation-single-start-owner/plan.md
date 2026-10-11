---
created: 2026-10-11
status: done
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
legacy_specs: []
---

# Implementation Plan: Start a feeder-created automation task only once

## Overview

An automation can create its task in a step that another step pulls from.
`CreateTask` runs that feeder pull before it returns, and the pull's
`task.moved` auto-starts the task when the pulling step has `auto_start_agent`.
The automation then starts the same task itself. With the session ceiling full,
the second start is refused as a conflicting deferral; with free capacity, it
fails because the first start is still preparing the workspace. Either failure
fails the run and deletes the task together with the first start. Issue #4323
reports the gap.

## Scope

### In scope

- Mark a task an automation creates as owned by the automation dispatch for its
  initial start.
- Skip workflow auto-start for a task that carries the marker.
- Remove the marker once the automation dispatch resolves, and clear markers a
  stopped process left behind at startup.
- Accept the marker only from the automation create path.
- Record the single start owner in the session ceiling design.

### Out of scope

- Continuation runs, which reuse an existing task.
- Changes to the ceiling deferral comparison or to automation failure cleanup.
- Rendered UI.

## Technical approach

`prepareAutomationTask` sets the internal `AutomationStartOwned` field of its
create request, and `CreateTask` stamps `automation_start_owned` when it builds
the task, so the marker is present before the feeder pull publishes
`task.moved`. Ordinary create and metadata update requests can neither add nor
remove the key, so a caller cannot keep its own task from auto-starting. `autoStartTaskForLoadedStep` returns before any launch
when the marker is set, and discards an auto-start-on-create claim it holds.
`autoStartAutomationTaskForRun` removes the marker when its dispatch returns,
whether the start ran, was queued by the ceiling, or failed. A task left without
a session, such as one whose queued start is later dropped, then auto-starts on
a later move like any other task. The removal runs on a bounded context
detached from the dispatch and publishes `task.updated`. Startup reconciliation
lists marked tasks, automation-origin ones included, and clears every
remaining marker before automation events are subscribed, since no dispatch
survives a restart.

## Tests

`event_handlers_workflow_automation_owned_start_test.go` covers
`TestAutoStartTaskForLoadedStepHonorsAutomationStartOwnership` and
`TestCreateAutomationTaskStampsStartOwnership`:
AC-AGENTS-SESSION-CEILING-001.10. An owned task on an auto-start step starts no
workflow launch, and an unmarked control task launches. The created automation
task carries the marker.
`automation_ceiling_defer_test.go` covers
`TestAutomationStartDeferredByCeiling_ReleasesStartOwnership`: after a dispatch
the ceiling queued, the task no longer carries the marker.
`TestReleaseAutomationStartOwnershipPublishesTaskUpdate` and
`TestStartupReleasesAbandonedAutomationStartOwnership` cover the release on a
cancelled dispatch context, its `task.updated`, and the startup cleanup.
`service_step_handoff_metadata_test.go` covers
`TestProtectedTaskMetadataForCreateRejectsAutomationStartOwnership`,
`TestBuildTaskStampsAutomationStartOwnershipOnlyFromInternalField`, and
`TestUpdateTaskMetadataPreservesAutomationStartOwnership`.

## E2E tests

The service tests use the real repository and ceiling controller; only the
agent manager is simulated. No rendered UI changes require browser coverage.

## Work orders

- [x] [Task 01: Single start owner for automation tasks](task-01-single-start-owner-for-automation-tasks.md)

## Verification results

The three orchestrator tests failed on the original code and pass with the
change. The existing deferred automation start tests pass with the marker on
their tasks.

## Risks

A move of an automation task before its dispatch resolves does not auto-start
it; the automation dispatch, or its ceiling replay, starts it instead.
