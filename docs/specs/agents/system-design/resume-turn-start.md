---
status: draft
system: agents
requirements:
  - REQ-AGENTS-RESUME-TURN-START-001
---

# Restart resume turn start system design

## Purpose and boundaries

This design describes how a restart-interrupted session reaches a real turn or an
explicit failure. It does not own the per-contributor guarantees; it owns the
combined outcome and points at the systems that provide each part.

- The orchestrator owns restart reconciliation, resume authorization, dynamic
  route state, and turn dispatch.
- The agent runtime and agentctl own the durable delivery journal, event
  projection, and the resumed transport/execution.
- The task repository owns sessions, executions, delivery inbox/projection rows,
  and the persisted route and turn state.
- The task system owns admission and workflow transitions for a message-triggered
  turn (see [resume prompt queue](../../tasks/system-design/resume-prompt-queue.md)).
- The agent resume contract owns provider identity, token preservation, and
  branch-loss continuation (see [Agent Resume and Runtime Recovery](agent-resume-runtime-recovery.md)).
- The dynamic routing contract owns candidate selection and route state (see
  [Dynamic Agent Routing Part 2](dynamic-agent-routing-02.md)).

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-RESUME-TURN-START-001` | [Restart reconciliation](#restart-reconciliation), [Failure and explicit outcomes](#failure-and-explicit-outcomes), [Waiting preservation](#waiting-preservation), [Delivery rebinding](#delivery-rebinding) |

## Components and responsibilities

- `apps/backend/internal/orchestrator/restart_reconciliation*.go` and
  `restart_recovered*.go`: classify sessions left `RUNNING`/`STARTING` after an
  unclean stop and drive recovery.
- `apps/backend/internal/orchestrator/resume_attempt.go`,
  `resume_continuation*.go`, `native_resume_*.go`, `explicit_resume_*.go`: build
  the resume request, apply the guarded `STARTING` transition, and fall back to an
  explicit continuation when native resume is refused.
- `apps/backend/internal/orchestrator/dynamic_restart_recovery*.go` and
  the dynamic route readers in `session_launch.go` / `task_operations.go`:
  reconcile a persisted route after restart and activate or explicitly fail it.
- `apps/backend/internal/agent/runtime/agentctl/journal` and the agent runtime
  event processor: retain and replay the delivery journal, and reattach the
  resumed execution's stream.
- `apps/backend/internal/task/repository/sqlite`: persist executions, delivery
  inbox/projection rows, and route/turn state.

The in-process agentctl replacement path is coordinated by
`apps/backend/internal/agent/runtime/agentctl/recovery_coordinator.go`.

## Restart reconciliation

At startup the backend sweeps sessions that are not idle but have no live
execution behind them. For each candidate it reconstructs the resume decision
from persisted evidence: the authenticated owner, the SQL generation, the
projected cursor, and the active submission. A resume that cannot reconstruct
that evidence fails closed and reports an explicit outcome.

A successful resume must attach a new execution and publish that execution's
first agent event. A resume response alone, without a linked execution and a
first event, is not a started turn.

For a dynamic session the sweep reads the persisted route state. A route that was
mid-switch when the process stopped is re-evaluated against the current
candidate list. It resolves to an activated route (resumed or relaunched on a
healthy candidate) or to an explicit failure. The pending state is never the
terminal state.

## Failure and explicit outcomes

- Native resume refused: the orchestrator issues an explicit continuation
  (continue from history) that starts a real turn on the same task session and
  reflects the new instruction.
- Cleanup unproven: when the previous runtime's cleanup cannot be confirmed, the
  resume reports the cleanup failure and does not launch a replacement (owned by
  Agent Resume and Runtime Recovery).
- Route cannot be reconciled: the route records an explicit failure that carries
  the cause and an applicable recovery action, rather than remaining pending.
- The three outcomes (resumed, relaunched, explicit failure) are distinguishable
  in the persisted session and execution state.

## Waiting preservation

Sessions parked on a parent, owner, blocker, or paused task are not restart
recovery candidates. The sweep preserves their wait, reference, and intent and
performs zero automatic re-dispatch. They advance only on an answer or an
explicit resume. A later explicit resume follows the normal guarded path.

## Delivery rebinding

When a primary session is handed over, inbound delivery for the task is bound to
the new generation and acknowledged. The original message text is retained and
no duplicate first run is created. Duplicate submissions, replayed events, and
sequence errors are counted separately (see Observability), so a rebinding is
distinguishable from a new turn.

## Persistence

- Session execution identity, route state, and turn state persist across the
  restart in SQLite.
- The delivery journal and projection cursors persist so a resumed execution
  reattaches to the retained stream.
- No durable state is blanked on a failed pre-session launch.

## Observability

- `agent_restore_attempts_total`, `agent_restore_context_truncated_total`,
  `agent_restore_recovery_required_total` (owned by `runtime/lifecycle/metrics/`).
- `agent_delivery_submissions_total`, `agent_delivery_duplicate_submissions_total`,
  `agent_delivery_replayed_events_total`, `agent_delivery_sequence_errors_total`,
  `agent_delivery_uncertain_submissions_total` (owned by `internal/agentctl/journal/`).
- `agent_delivery_inbox_lag_events`, `agent_delivery_projection_lag_events`
  (owned by `internal/task/repository/sqlite/agent_delivery_metrics.go`).
- `agent_runtime_recovery_attempts_total`, `agent_runtime_recovery_outcomes_total`,
  `agent_runtime_recovery_duration_seconds`, `agent_runtime_recovery_in_progress`
  (owned by `internal/agent/runtime/agentctl/recovery_coordinator.go`).

Recovery IDs and runtime epochs stay in structured logs, never in metric keys.

## Related documents

- [Agent Resume and Runtime Recovery](agent-resume-runtime-recovery.md)
- [Dynamic Agent Routing Part 2](dynamic-agent-routing-02.md)
- [Resume prompt queue](../../tasks/system-design/resume-prompt-queue.md)
- Plans: [durable-agent-sessions](../../../plans/durable-agent-sessions/plan.md),
  [durable-agent-session-reconciliation](../../../plans/durable-agent-session-reconciliation/plan.md),
  [agentctl-journal-shutdown-recovery](../../../plans/agentctl-journal-shutdown-recovery/plan.md).
