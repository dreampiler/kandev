---
created: 2026-10-13
status: implemented
requirements:
  - REQ-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001
system_design:
  - ../../specs/platform/system-design/restart-stranded-route-recovery.md
legacy_specs: []
---

# Fix plan: Restart-stranded route recovery

## Overview

After a backend restart, a dynamic-profile session can hold a route projected at
`starting`, an open restart-interrupted recovery block for its turn, and an open
orphan turn. Explicit history continuation was refused as
`dynamic_route_pending`, and the route action was refused as "route actions
require a settled turn", so recovery waited on routing and routing waited on
turn settlement. This package admits the guarded settlement of a provably
pre-boot orphan turn on a stranded `starting` route before a route action.

The platform owns this package because it owns recovery of delivery state across
backend restarts and the convergent disposition of the recovery entry paths. One
sequential work order covers the predicate, the companion tests, and the
documentation.

## Evidence and root cause

Inspected revision: `ef4924f08d` on the task worktree. The reported production
reproduction is a restart on commit `94e2cfbfa9` after a 2026-10-13 15:57 KST
replacement, where three dynamic sessions recorded `unresolved_durable_work`
recovery blocks, `dynamic_route_states.status = action_required` against
`task_sessions.route_state = starting`, a peer message refused as
`unresolved_durable_work`, `continue_from_history` refused as
`dynamic_route_pending`, and `try_next` refused with "route actions require a
settled turn".

The static code cause is the mutual block between two guards:

- The route-action guard
  (`apps/backend/internal/orchestrator/service.go:1806-1824`) reads the active
  turn and, when one exists, calls
  `settleOrphanTurnForRouteAction`. On refusal it returns
  `ErrRouteActionActiveTurn` (`service.go:55`).
- `settleOrphanTurnForRouteAction`
  (`apps/backend/internal/orchestrator/restart_stranded_route_recovery.go:148-181`)
  admitted only a parked route through
  `dynamicRouteParked` (`restart_stranded_route_recovery.go:60-67`, the set
  `action_required`, `waiting`, `retry_wait`, `waiting_for_reset`). A `starting`
  route was refused, so the orphan turn could not be settled.
- The explicit recovery admission
  (`apps/backend/internal/orchestrator/task_operations.go:3869-3872`) refuses a
  non-active route as `dynamic_route_pending` unless the route is a stranded
  `starting` route owned by an explicit recovery action
  (`interrupted_turn_recovery.go:254-265`).

The installed restart-stranded recovery helpers (commit `27f0f52831`), the retry
continuation (commit `410c924eb2`), and #271 (commit `2f10c1afb3`) already cover
the parked route; this package adds only the guarded `starting` case and does
not reimplement them.

## Confirmed and unconfirmed evidence

Confirmed from source:

- The route-action refusal and the parked-only settlement predicate above.
- Runtime liveness is a real process probe
  (`apps/backend/internal/agent/runtime/lifecycle/manager_interaction.go:2122-2166`);
  a dead pre-boot execution has no agentctl client and probes as not running, so
  it is not misdetected as live. The startup liveness grace is bounded to 75
  seconds and does not cover a pre-boot launch.

Stress-tested with a temporary fixture during implementation: a session whose
route is `starting` with an open restart-interrupted block and no live runtime
settles its orphan turn and admits both entry points; the same state without the
block, or with a live runtime, is refused. The temporary fixture was not
committed.

Not confirmed by this worker: the exact guard that fired in the live
reproduction runs, because the raw recovery logs were not provided. The
temporary fixture covers the two admissible variants (route projection
`starting`, and durable/projection disagreement); an unreadable or
non-restart-interrupted recovery block stays refused by design.

## Requirement reconciliation

Platform owns the package. The
[requirements](../../specs/platform/requirements/restart-stranded-route-recovery.md)
extend the platform restart-recovery contract; the route state machine
([dynamic agent routing](../../specs/agents/system-design/dynamic-agent-routing-02.md)),
the recovery block and manual resume
([interrupted prompt recovery](../../specs/platform/system-design/interrupted-prompt-recovery.md)),
and turn settlement
([session turn settlement](../../specs/tasks/requirements/session-turn-settlement.md))
are used, not changed. No new ADR is needed: the change is one guarded predicate
inside an existing recovery helper.

## Scope

### In scope

- Admit the guarded settlement of an orphan turn on a stranded `starting` route
  before a route action.
- Companion unit tests for the previous-boot settlement, the live-runtime and
  no-block refusals, and the unchanged parked behavior.
- The English requirements, system design, plan, and work order.

### Out of scope

- New permission models, settings screens, recovery brokers, recurring checkers,
  or forced provider traffic.
- Blanket settlement, clearing unresolved work or route state, or resuming every
  interrupted session.
- Changing route selection, dynamic-profile configuration, prompt admission, or
  recovery-block creation and resolution.
- Office scheduler capacity and dispatch, and any UI or public documentation
  change.

## Technical approach

In `apps/backend/internal/orchestrator/restart_stranded_route_recovery.go`, add
`routeActionMaySettleOrphanTurn` and call it from
`settleOrphanTurnForRouteAction` in place of the bare `dynamicRouteParked` check:

- A parked route returns true, preserving the existing behavior.
- A `starting` route returns true only when `GetOpenSessionRecoveryBlock` returns
  an open block whose reason is in `interruptedTurnRecoveryReasons`.
- Every other route state returns false.

`dynamicRouteParked` keeps its meaning for any future caller. All existing
safety guards in `settleOrphanTurnForRouteAction` remain: the session must not be
`RUNNING` or `STARTING`, no runtime may be live, no resume attempt may be active,
and no queued dispatch or steer may be in flight. The change does not touch
`session_launch.go`, `task_operations.go`, `service.go`, or
`dynamic_policy_recovery.go`, so there is no overlap with the sibling tasks
`2192e751` (memory-aware launch deferral) or `4a8de330` (review-run cleanup).

## Tests

New companion tests live in
`apps/backend/internal/orchestrator/restart_stranded_route_recovery_test.go`.

| Acceptance | Test |
| --- | --- |
| 001.1, 001.5 | `TestRestartStrandedStartingRoute_SettlesPreviousBootOrphanTurn`: `starting` route, restart-interrupted block, no runtime; history-continuation admission succeeds, the `try_next` route action settles the turn and runs once. |
| 001.2 | `TestRestartStrandedStartingRoute_LiveRuntimeKeepsGuard`: live runtime keeps `ErrRouteActionActiveTurn`. |
| 001.2 | `TestRestartStrandedStartingRoute_NoInterruptedBlockKeepsGuard`: no restart-interrupted block keeps `ErrRouteActionActiveTurn`. |
| 001.3 | Existing `TestRestartStrandedDynamicRoute_*` parked tests remain unchanged. |

## E2E tests

None. The change is a backend-only guard with no browser surface; the guarded
predicate and both entry points are exercised by the Go package tests.

## Work orders

- [x] [Task 01: Settle a pre-boot orphan turn on a stranded starting route](task-01-strand-turn-settlement.md)

## Verification results

Run from `apps/backend` on the task worktree:

- `go test ./internal/orchestrator/ -run 'TestRestartStranded|TestApplyRouteAction' -count=1 -trimpath`: passed (`ok`, 2.148s).
- `go test ./internal/orchestrator/ -count=1 -trimpath`: the package fails on
  three pre-existing Windows-environment tests
  (`TestHandleAgentReady_PassthroughAttachmentOnlyOrdinaryMessageUsesAttachmentAwarePrompt`,
  `TestHandleAgentReady_PassthroughQueuedAttachmentUsesAttachmentAwarePrompt`,
  `TestAutoPromotionPreservesLogicalProfileAndNewestSession/same_logical_successor`).
  The same three fail at `HEAD` without this change, so they are not a
  regression. They are Windows path-separator and environment failures in the
  attachment prompt path and are unrelated to route settlement.
- `go build ./...`: passed.
- `go vet ./internal/orchestrator/`: passed.
- `python3 scripts/list-docs.py validate`: passed.
- `python3 scripts/lint-spec-files.py --all`: passed.

## Documentation impact

Internal specifications and delivery records only. No user-facing command,
configuration, API, or UI change, so no public documentation change is needed.

## Risks

- A blanket `starting` settlement could close a turn that a launch in this boot
  still owns; the restart-interrupted block fence and the liveness/state guards
  prevent it.
- An unreadable recovery block must fail closed; the predicate returns false on
  any read error.
- The companion tests must not pass without the fence; the no-block and
  live-runtime tests assert the refusal.
