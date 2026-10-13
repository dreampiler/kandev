---
status: draft
system: platform
requirements:
  - REQ-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001
---

# Restart-stranded route recovery System Design

## Purpose and boundaries

This design owns the guarded settlement of an orphan turn before a route action
when a backend restart strands a dynamic route at `starting`. It uses, but does
not own, the dynamic route state machine and its route-action admissibility
([dynamic agent routing](../../agents/system-design/dynamic-agent-routing-02.md)),
the recovery block and explicit history continuation
([interrupted prompt recovery](interrupted-prompt-recovery.md),
[durable restart reconciliation](durable-restart-reconciliation.md)), and turn
settlement ([session turn settlement](../../tasks/requirements/session-turn-settlement.md)).

The behavior is one predicate extension in the existing orchestrator recovery
helper. It adds no new component, no new persisted state, and no new
configuration.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001` | [Components and responsibilities](#components-and-responsibilities), [Control flow](#control-flow) |
| `AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.1`, `.2` | [Control flow](#control-flow), [Failure and recovery](#failure-and-recovery) |
| `AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.3` | [Components and responsibilities](#components-and-responsibilities) |
| `AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.4` | [Failure and recovery](#failure-and-recovery), [Security](#security) |
| `AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.5` | [Control flow](#control-flow) |

## Components and responsibilities

- **Route-action guard** (`rejectRouteActionDuringActiveTurn`): detects an active
  turn, attempts the guarded settlement, and returns the settled-turn refusal
  when settlement is not admissible. It holds the session's cancel-in-flight
  guard for the active-turn read only.
- **Orphan-turn settlement** (`settleOrphanTurnForRouteAction`): closes the open
  turn rows through the task turn service and verifies no active turn remains.
- **Settlement admissibility** (`routeActionMaySettleOrphanTurn`): admits a
  parked route unchanged, admits a `starting` route only with an open
  restart-interrupted recovery block, and refuses every other route state.
- **Recovery block store** (`GetOpenSessionRecoveryBlock`): resolves the open
  block for the session's current incarnation and harness generation. It is the
  existing proof that a restart left the turn unsettled.
- **Runtime liveness** (`sessionRuntimeLive`): probes the actual agent process
  for the session. A live runtime keeps the turn owned.

## Data and contracts

No new model, column, event, or API. The design reads existing values:

- `task_sessions.route_state` (`starting`, `active`, `action_required`, ...).
- `task_sessions.state` (`RUNNING`, `STARTING`, `WAITING_FOR_INPUT`, ...).
- `session_recovery_blocks` open row and its reason.
- The session's open turn rows.

The settlement reuses `AbandonOpenTurns` and `GetActiveTurn` from the task turn
service.

## Control flow

```text
route action requested
  -> rejectRouteActionDuringActiveTurn
     -> active turn read
        -> no active turn: proceed
        -> active turn: settleOrphanTurnForRouteAction
             -> routeActionMaySettleOrphanTurn
                  parked route            -> true (unchanged)
                  starting route          -> open restart-interrupted block?
                                               yes -> continue
                                               no  -> refuse
                  any other route state   -> refuse
             -> session not RUNNING/STARTING
             -> runtime not live
             -> no resume attempt, queued dispatch, or steer in flight
             -> AbandonOpenTurns; verify no active turn remains
             -> settled: route action proceeds
             -> refused: ErrRouteActionActiveTurn (CONFLICT)
```

After a successful settlement the route action transitions the route, so the
explicit history continuation is no longer refused as `dynamic_route_pending`.
Settlement itself does not mutate route state.

## Failure and recovery

- A missing or unreadable recovery block is treated as not admissible, so an
  unreadable row never settles a turn that might still be starting in this boot.
- A live runtime, a `RUNNING`/`STARTING` session, an active resume attempt, and
  in-flight queued dispatch or steer all keep the existing refusal.
- A settlement that fails to close the turn returns the refusal and leaves the
  route and turn unchanged.
- The settlement never fabricates a completion event; abandoned turns are
  closed as abandoned, not successful.
- A restart alone is not evidence that every turn is dead, so the `starting`
  admission is fenced by the restart-interrupted recovery block plus liveness,
  not by boot alone.

## Persistence

The design adds no migration and no new table. It reads and writes the existing
`task_sessions`, `session_recovery_blocks`, and turn rows through existing
repositories. No durable route state is written by the settlement.

## Security

Caller identity, session authorization, and route-generation fences are
unchanged. The settlement runs inside the existing route-action authorization
and lock ordering. It changes no permission model and exposes no new data.

## Observability

The existing "settled orphaned turn on a parked dynamic route before route
action" log line reports the settlement, including the route state. No new
metric, label, or identifier is introduced.

## Related decisions

- [Durable restart reconciliation](durable-restart-reconciliation.md).
- [Interrupted prompt recovery](interrupted-prompt-recovery.md).
- [Dynamic agent routing](../../agents/system-design/dynamic-agent-routing-02.md).
