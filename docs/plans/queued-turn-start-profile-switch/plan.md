---
created: 2026-10-20
status: done
requirements:
  - REQ-TASKS-WORKFLOW-PROFILE-SESSIONS-001
system_design:
  - ../../specs/tasks/system-design/workflow-profile-session-lifecycle.md
legacy_specs: []
---

# Implementation Plan: Retain the queued claim guard through a profile switch

## Overview

A queued prompt is claimed while the source session's lifecycle guard is held,
and its after-claim callback runs `on_turn_start` before the guard is released.
When that action moves the task to a step with a different agent profile and
the source step parks its session, profile-switch parking tried to acquire the
same non-reentrant guard and the queued dispatch deadlocked. The task never
reached the destination step, the prompt was never delivered, and the session's
queue stayed blocked. One work order passes the existing guard ownership into
the synchronous workflow handling.

## Scope

In scope: `queuedMessageAfterClaim` in
`apps/backend/internal/orchestrator/event_handlers_agent.go`, one permanent
orchestrator test, and the profile-session lifecycle requirement and design.

Excluded: guard acquisition, session identity checks, stamped parking, deferred
runtime teardown, and recovery of an already-stalled process.

## Technical approach

`parkSessionForProfileSwitch` already supports callers that own the guard
through `withWorkflowProfileSwitchGuardHeld`; it then commits the park claim
in place and schedules runtime teardown after the caller returns. The queued
after-claim callback now marks its context the same way before calling
`processOnTurnStartViaEngine`.

## Work orders

- [x] [Task 01: Retain the queued claim guard through a profile switch](task-01-retain-claim-guard.md)

## Changed-path coverage

The work order owns the `event_handlers_agent.go` change, the permanent
`queued_turn_start_profile_switch_test.go` regression test, the extended
`docs/specs/tasks/requirements/workflow-profile-session-lifecycle.md`
acceptance criteria, and the
`docs/specs/tasks/system-design/workflow-profile-session-lifecycle.md` control
flow step 11.

## Verification

`TestQueuedTurnStartProfileSwitchParksSourceUnderClaimGuard` drains a real
queued prompt through the claim and after-claim callback on both the legacy and
workflow-engine paths. It times out on the previous source with the dispatch
goroutine blocked in `parkSessionForProfileSwitch` and passes with the change.

## Risks

- Marking the guard as held outside its real owner would let parking race
  terminal callbacks; the marker is set only inside the synchronous
  after-claim callback that runs under the claim's guard.
