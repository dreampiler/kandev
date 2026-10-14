---
id: "01-strand-turn-settlement"
title: "Settle a pre-boot orphan turn on a stranded starting route"
status: completed
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001
acceptance_criteria:
  - AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.1
  - AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.2
  - AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.3
  - AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.4
  - AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.5
system_design:
  - ../../specs/platform/system-design/restart-stranded-route-recovery.md
---

# Task 01: Settle a pre-boot orphan turn on a stranded starting route

## Summary

Extend the route-action orphan-turn settlement predicate to admit a stranded
`starting` route when an open restart-interrupted recovery block proves the turn
predates the current backend boot. Keep the parked-route behavior and every
existing safety guard unchanged.

## In scope

- Add `routeActionMaySettleOrphanTurn` and call it from
  `settleOrphanTurnForRouteAction`.
- Add companion unit tests for the previous-boot settlement, the live-runtime
  refusal, the no-block refusal, and the unchanged parked behavior.
- Record results in this work order and the plan.

## Out of scope

- Blanket settlement, route-state forcing, and recovery-block resolution changes.
- Route selection, profile configuration, prompt admission, and Office
  capacity or dispatch.
- New metrics, gates, integration/E2E collections, or UI.

## Acceptance

1. A `starting` route with an open restart-interrupted recovery block and no live
   runtime settles its orphan turn before a route action, and the action runs
   once. Explicit history continuation is no longer refused.
2. A `starting` route without a restart-interrupted block, or with a live
   runtime, is refused with `ErrRouteActionActiveTurn`.
3. A parked route keeps its existing settlement behavior.
4. Settlement fabricates no terminal outcome and changes no owner, generation,
   cursor, active submission, replay/acknowledgement identity, queued message,
   or conversation.

## TDD procedure

1. Add a `starting`-route fixture beside `newStrandedRouteFixture` that leaves
   the route at `starting` and, optionally, seeds the restart-interrupted
   recovery block.
2. Assert the pre-boot orphan turn settles and the route action proceeds once,
   and that history-continuation admission succeeds.
3. Assert a live runtime and a missing block each keep the refusal.
4. Apply the predicate change and confirm the existing parked tests still pass.

## Verification

```bash
(cd apps/backend && go test ./internal/orchestrator/ -run 'TestRestartStranded|TestApplyRouteAction' -count=1 -trimpath)
(cd apps/backend && go build ./...)
(cd apps/backend && go vet ./internal/orchestrator/)
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
```

The full `internal/orchestrator` package has three pre-existing Windows
environment failures that fail at `HEAD` without this change and are not a
regression; see the plan's verification section.

## Files likely touched

- `apps/backend/internal/orchestrator/restart_stranded_route_recovery.go`
- `apps/backend/internal/orchestrator/restart_stranded_route_recovery_test.go`
- This work order and its sibling plan, and the specification pair.

## Dependencies

None.

## Risks

- A too-broad `starting` admission could close a live in-boot turn; the block
  fence and the liveness/state guards prevent it.
- A read error on the recovery block must fail closed.

## Parallelism

`sequential`

## Inputs

- [Restart-stranded route recovery requirements](../../specs/platform/requirements/restart-stranded-route-recovery.md).
- [Restart-stranded route recovery design](../../specs/platform/system-design/restart-stranded-route-recovery.md).
- [Interrupted prompt recovery](../../specs/platform/system-design/interrupted-prompt-recovery.md).
- [Dynamic agent routing](../../specs/agents/system-design/dynamic-agent-routing-02.md).
- `restart_stranded_route_recovery.go`: the settlement and parked predicate.
- `service.go`: the route-action guard and `ErrRouteActionActiveTurn`.

## Results

Implemented `routeActionMaySettleOrphanTurn`; `settleOrphanTurnForRouteAction`
now admits a parked route as before and a `starting` route only with an open
restart-interrupted recovery block. All existing guards are unchanged. The
companion tests cover the previous-boot settlement, the live-runtime refusal,
the missing-block refusal, and the parked behavior. Verification results are
recorded in the plan.
