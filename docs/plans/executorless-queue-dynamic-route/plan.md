---
created: 2026-10-20
status: done
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
system_design:
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
legacy_specs: []
---
# Implementation Plan: Queued Messages For A Dynamic Route Awaiting Action

## Overview

A message queued for a dynamic session that cannot run until the user applies
a route action was requeued forever. Each automatic drain reserved the entry,
failed in `ensureSessionRunning` with "no executor record", classified the
failure as a launch still in progress, and requeued it at the head. Nothing
told the user, and nothing would ever launch the session on its own.

[Requirements](../../specs/agents/requirements/dynamic-agent-routing.md)
AC 001.25 and 001.26 define the corrected delivery contract. The
[design](../../specs/agents/system-design/dynamic-agent-routing-02.md) records
where the block sits.

## Evidence and cause

- A session was WAITING_FOR_INPUT with route state `action_required`, reason
  `orphaned_starting_route`, and no `executors_running` row. The restart orphan
  sweep had released its stranded `starting` route as AC 001.14 requires.
- `attemptColdResume` returns `errSessionAwaitingRuntimeLaunch` for any session
  without an executor row. `classifyEnsureSessionRunningFailureForPrompt` maps
  it to `ErrSessionRuntimeUnavailable`, and `handleQueuedMessageExecutionError`
  requeues that class. The class is correct for a launch that has not run yet,
  but a route that awaits a user action has no launch owner.
- An explicit `session.recover` with `action=resume` returned
  `dynamic_route_pending`. AC 001.13 keeps that guard for every non-active
  route state other than `starting`, so the refusal matches the contract. The
  route recovery action (Retry or Try next) is the supported recovery for
  `action_required`.
- The session became primary again when a workflow step that names its profile
  reused it through `selectReusableWorkflowSession`. Reusing a nonterminal
  session is the documented workflow policy; the defect is only the delivery
  loop that followed.

## Scope

- Skip automatic queue reservation while the route is `action_required` and the
  executor row is absent, in both drain cores that reserve the queue head.
- When a queued dispatch fails with `errSessionAwaitingRuntimeLaunch` and the
  session's `starting` route is stranded in WAITING_FOR_INPUT, release the
  route as the orphan sweep would and restore the entry for manual recovery.
- No new status, setting, or flag. The route recovery banner already renders
  `action_required` with Retry, Try next, and Stop.

## Out of scope

- Moving a non-terminal session's queue when the user selects another primary.
  Queued messages target the conversation the user addressed, as
  `REQ-TASKS-QUEUED-SESSION-OWNERSHIP-001` requires for explicit execution.
- Changing the reuse selector, the `dynamic_route_pending` guard, or the
  runtime-unavailable requeue for sessions without a dynamic route.

## Tests

| Test | Covers |
| --- | --- |
| `TestQueueDrainSkipsSessionWhoseDynamicRouteAwaitsAction` | AC 001.25 |
| `TestQueuedDispatchToStrandedStartingRouteMarksActionRequired` | AC 001.26 |

Existing `TestQueueUserPrompt_RuntimeUnavailableSurvivesFastPathRedispatch`
keeps the non-dynamic requeue, and the orphan-sweep and explicit-recovery tests
keep AC 001.13 and 001.14.

## E2E tests

None. The change is a backend delivery decision with no new UI. The existing
route recovery banner renders the block.

## Work orders

- [x] [Task 01: Hold queued delivery while a dynamic route awaits action](task-01-hold-queue-for-route-action.md)

## Verification results

- RED before the fix: both new tests failed. The drain dispatched the entry to
  the `action_required` session, and the stranded `starting` route stayed
  `starting` after the failed dispatch.
- GREEN after the fix: both new tests pass, with the queue, prompt, orphan
  sweep, and explicit recovery tests in `internal/orchestrator`.

## Risks

- A session without a dynamic route that lost its executor row while
  WAITING_FOR_INPUT still uses the requeue path. That shape cannot be told
  apart from a profile-switch launch that has not run yet.
