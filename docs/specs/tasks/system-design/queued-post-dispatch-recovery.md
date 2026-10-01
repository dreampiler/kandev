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
| `AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.5` through `.7` | Prompt rollback ownership |

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

## Prompt rollback ownership

MCP prompt preparation records whether it created a new turn. Rejection deletes
its rejected message, then atomically removes only that exact unreferenced open
turn while the session remains CREATED, WAITING_FOR_INPUT, or IDLE. Existing,
referenced, completed, or RUNNING-adopted turns survive. Successful removal
uses the existing TurnRemoved event. An accepted-dispatch error retains the
transcript and turn and reports sent.

Native missing-turn compensation retains the claim's original identity and
execution and the exact revision written by its own claim/turn insertion. It
does not adopt a later owner's row after a callback. Restoration requires an
unaccepted claim, matching task/session/incarnation/execution, RUNNING state,
no error or completion, unchanged revision, no replacement cached turn, and
the existing cancellation guard. The existing snapshot CAS must still succeed;
otherwise compensation preserves the winner. Normal queue reservation and
retry retain FIFO, Auto-run OFF, and accepted long-running provider protection.

This repair does not change schema or global CAS policy. Arbitrary same-timestamp,
same-identity metadata changes remain limited by existing revision precision.

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

Prompt ownership verification uses actual MCP and native claim/rollback
functions with temporary SQLite and the existing fake runtime boundary. Cover
own-turn cleanup and adoption, successor revision/execution, accepted prompts,
CAS zero rows, FIFO retry, Auto-run OFF, long-running provider preservation,
and TurnRemoved event with database readback. Use focused race tests without
listeners or operating processes.
