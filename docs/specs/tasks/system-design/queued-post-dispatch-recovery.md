---
status: draft
system: tasks
requirements:
  - REQ-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001
---

# Queued Post-Dispatch Recovery System Design

## Purpose and boundaries

`orchestrator.Service.executeQueuedMessageWithReservation` calls
`finishQueuedMessageExecution`, then triggers another guarded drain only when
`err == nil`. `finishQueuedMessageExecution` already recognizes an
`acceptedPromptDispatchError` and acknowledges the source row, preventing
duplicate execution. That error branch does not request a successor drain.
At 12:09-12:12 KST on 2026-10-01, three queued prompts followed exactly this
accepted-error path after synthetic completion; their remaining queues did
not advance before the 12:32 backend restart.

The task system owns row acknowledgement and successor eligibility. The agent
system's terminal-stall classification remains unchanged here.

## Requirement mapping

| Criteria | Sections |
| --- | --- |
| `AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.1` | Accepted-row settlement |
| `AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.2` through `.4` | Successor eligibility and recovery |

## Accepted-row settlement

Keep the accepted-dispatch receipt authoritative. The original queue ID is
acknowledged once through the existing ordinary/lifecycle distinction even
when its post-dispatch callback returns an error. Do not turn that error into
an unaccepted retry or copy the accepted content to a new row. Publish the
queue status after settlement through the existing identity-aware path.

## Successor eligibility and recovery

After acknowledgement and release of the current dispatch claim, schedule one
guarded successor evaluation for the exact task/session/incarnation. Reuse
`drainQueuedMessageForPromptableSession` and its WIP, clarification,
Auto-run, cancellation, in-flight, and session-promptability checks. Do not
perform the evaluation while the failed prompt still owns the session guard.
Coalesce concurrent ready/turn-finished/restart signals under the existing
queue reservation so only one trigger claims the FIFO head.

If the session is terminal or its runtime is unavailable, consult the
existing eligible primary-session transfer/recovery path. Transfer only when
its normal ownership checks permit it; otherwise retain the queue and expose
the blocked recovery state on the task/session surface. A later valid resume
or session recovery re-evaluates the same pending head. Never silently turn a
terminal failed session into an automatically restarted agent, and never
advance a workflow step from a synthetic completion.

## Failure and observability

Log the accepted queue ID and successor disposition (`dispatched`,
`auto_run_off`, `not_promptable`, `recovery_required`, or `read_failed`) with
bounded fields. A failed queue-state read leaves rows durable for the next
normal retry; it does not report successful delivery. No message body or
credential is logged.

## Verification

Use focused existing orchestrator queue-dispatch recovery tests with a fake
agent manager and no listener. Exercise accepted post-dispatch failure with
one later row, terminal session/no eligible primary, eligible primary,
Auto-run OFF, concurrent ready signal, and restart reconciliation. The owner
forbids listener-opening tests, the full Go suite, and E2E.
