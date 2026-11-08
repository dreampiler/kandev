---
created: 2026-10-20
status: done
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
system_design:
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
legacy_specs: []
---
# Implementation Plan: Route Actions On An Orphaned Route Without An Executor

## Overview

The restart orphan-route sweep marks a WAITING_FOR_INPUT session whose route is
still `starting` and which has no `executors_running` row as `action_required`
with reason `orphaned_starting_route` (AC 001.14). Retry and Try next on that
session failed and returned `action_required` with reason
`route_action_launch_failed`, so a message held in the session queue was never
delivered.

[Requirements](../../specs/agents/requirements/dynamic-agent-routing.md)
AC 001.27 defines the corrected contract. The
[design](../../specs/agents/system-design/dynamic-agent-routing-02.md) failure
modes record the relaunch rules.

## Evidence and cause

- Without an executor row the session has no `AgentExecutionID`.
  `launchDynamicRouteAction` passes that empty ID to
  `stopDynamicRelaunchPredecessor`, and `executor.StopExecution("")` returns the
  bare `executor.ErrExecutionNotFound`. The predecessor check accepted only
  `agentruntime.ErrNotFound`, so it logged
  `failed to stop dynamic fallback predecessor` and refused the relaunch.
- After that check passes, a manual relaunch used the prompt launch mode, which
  goes through `ensureSessionRunning`. `attemptColdResume` has nothing to resume
  without an executor row and returns `errSessionAwaitingRuntimeLaunch`, so the
  relaunch still failed.
- The chat handler's atomic queued send writes the held message's transcript
  row when it queues the entry. `dynamicRelaunchPrompt` replays the latest
  durable user message, which was that held message. The queue drain then
  delivered it a second time.
- `executor.StopExecution` wraps a genuine teardown failure in
  `ErrExecutionNotFound` as well. Only the bare sentinel and the runtime
  not-found error mean the predecessor is absent.
- The operator evidence supplied with this defect also records a relaunch step
  failing with `InvalidStatus: server rejected WebSocket connection: HTTP 503`.
  Neither `swap-primary` nor that error string appears anywhere in this
  repository, so the relaunch decision changed here does not raise it. The
  changed code (predecessor classification, launch-mode choice, prompt replay)
  opens no WebSocket (`dynamic_launch.go:1706-1711`,
  `dynamic_launch.go:1733-1752`, `dynamic_launch.go:1899-1927`), and the
  backend's WebSocket clients use `gorilla/websocket`
  (`internal/agent/runtime/agentctl/client.go:17`), whose handshake failure text
  is different.
- The 503 matches the required-persistence gate instead. While required
  persistence is unhealthy, the gate answers stateful requests with 503
  `persistence_unavailable`
  (`internal/backendapp/persistence_middleware.go:18-31`). `/ws` is not on the
  excluded list, so the upgrade is rejected too
  (`persistence_middleware.go:34-38`, pinned by
  `persistence_middleware_test.go:133-139`). The operator record for the
  reported timestamp places it inside such an unhealthy window, when other
  `/api/v1` calls also returned 503.
- That makes the WebSocket rejection a separate cause, not evidence that the
  orphaned route relaunch still fails. This change neither fixes nor regresses
  it. No reproduction with healthy required persistence has been observed; if
  one appears, it is a new defect. Persistence unavailability itself is
  operational scope.

## Scope

- Treat an empty predecessor execution ID, the bare
  `executor.ErrExecutionNotFound`, and `agentruntime.ErrNotFound` as an absent
  predecessor. Other stop errors still refuse the launch.
- Use the prompt launch mode for a manual relaunch only when an executor row
  exists. Otherwise start a fresh execution, as an automatic fallback does.
- Skip user messages owned by a still-queued entry in both the replayed
  relaunch prompt and the dynamic continuation. The two lookups live in
  `dynamic_launch_orphan_route.go` so `dynamic_launch.go` stays within the file
  length rules.

## Out of scope

- The queue hold for an `action_required` route, the orphan sweep, and the
  route-action handler's claim and failure projection.
- A session whose only user messages are held in the queue. Its relaunch has no
  prompt to replay and fails visibly, as before.
- The `InvalidStatus: server rejected WebSocket connection: HTTP 503` failure
  reported next to the relaunch. It is the persistence gate rejecting a client
  WebSocket upgrade, not the orphaned-route decision
  (`persistence_middleware.go:18-38`); a reproduction with healthy persistence
  would be a new defect.

## Tests

| Test | Covers |
| --- | --- |
| `TestLaunchDynamicRouteAction_OrphanedRouteWithoutExecutorLaunchesAndDeliversHeldMessageOnce` | AC 001.27 Retry and Try next, held entry with and without a recorded transcript |
| `TestStopDynamicRelaunchPredecessor_ClassifiesStopOutcomes` | AC 001.27 absent predecessor and genuine stop error |

Existing `TestLaunchDynamicRouteAction_MarksRouteActionRequiredWhenRelaunchFails`
and `TestRelaunchDynamicTaskAfterFailure_DoesNotLaunchSuccessorWhenStopFails`
keep a genuine stop failure refusing the launch.

## E2E tests

None. The change is a backend relaunch decision with no new UI. The existing
route recovery banner issues the actions.

## Work orders

- [x] [Task 01: Relaunch an orphaned route without an executor](task-01-orphaned-route-relaunch.md)

## Verification results

Run from `apps/backend` on the branch head. The focused tests were compiled once
(`go test -c -o green.test.exe ./internal/orchestrator/`) and executed from the
binary, because the shared CPU lock made `go test` runs wait behind other lanes.

- RED: with `dynamic_launch.go` and `event_handlers_agent.go` restored to the
  base revision, `TestLaunchDynamicRouteAction_OrphanedRouteWithoutExecutor...`
  failed all four subtests with `dynamic route action successor launch failed`,
  and `TestStopDynamicRelaunchPredecessor_ClassifiesStopOutcomes/empty_predecessor_id_is_already_gone`
  failed with `stopDynamicRelaunchPredecessor("") = false, want true`.
- GREEN after restoring the change:
  - `green.test.exe -test.run TestLaunchDynamicRouteAction_OrphanedRouteWithoutExecutorLaunchesAndDeliversHeldMessageOnce -test.count=1` passed (4 subtests).
  - `green.test.exe -test.run TestStopDynamicRelaunchPredecessor_ClassifiesStopOutcomes -test.count=1` passed (3 subtests).
  - `green.test.exe -test.run TestLaunchDynamicRouteAction_` passed, including
    `TestLaunchDynamicRouteAction_MarksRouteActionRequiredWhenRelaunchFails`.
  - `green.test.exe -test.run TestRelaunchDynamicTaskAfterFailure_` passed, including
    `TestRelaunchDynamicTaskAfterFailure_DoesNotLaunchSuccessorWhenStopFails`.
  - `green.test.exe -test.run TestLaunchPreparedDynamicRelaunch` and
    `green.test.exe -test.run TestReconcileOrphanedDynamic` passed.
- `go build ./internal/orchestrator/` and `gofmt -l` on the changed Go files
  produced no output.

## Risks

- A manual relaunch for a session without an executor row now starts a fresh
  execution instead of failing in the resume path. That matches the automatic
  fallback, which already used this path.
- A queue-owned user message is also skipped when an automatic fallback builds
  its continuation, not only for a manual route action. With auto-run off, the
  successor starts from the earlier durable prompt and the held message reaches
  it only when the queue releases it, so the message is delayed, never
  duplicated. The relaunch subtest pins the exclusion at the launch boundary.
