---
status: draft
system: tasks
requirements:
  - REQ-TASKS-CHILD-STALL-001
  - REQ-TASKS-CHILD-STALL-002
  - REQ-TASKS-CHILD-STALL-003
---

# Child-turn Stalled Signal System Design

## Boundary and evidence

The task system owns the candidate and delivery identity. The following source
was inspected on 2026-10-01; proposed additions below are not shipped behavior.

- `internal/task/service/service_turns.go`: `StartTurn` and `ReserveTurn` enter
  `createTurn`; `CompleteTurn` commits completion before publishing
  `events.TurnCompleted`. `AbandonOpenTurns` publishes that event too.
- `internal/events/bus/bus.go`: `EventBus` exposes publication/subscription but
  no durable consumer acknowledgement or replay contract. A bus publish alone
  cannot satisfy this feature's recovery requirement.
- `internal/task/models/step_transitions.go` and
  `internal/task/repository/sqlite/step_transitions.go` own the task ledger.
  Use its task entry identity, not a step label or timestamp comparison. See
  [transition ledger](workflow-task-step-transition-ledger.md).
- `internal/office/repository/sqlite/wake_receipts.go` tracks terminal child
  waves. Do not reuse its `parent_child_wake_receipts` identity for turn alerts.
- `internal/orchestrator/messagequeue/` owns queue persistence and recovery.
  `repository_dispatch_recovery.go` explicitly notes that the agent protocol
  lacks a prompt idempotency key. This design guarantees one queue/history
  item, not exactly-once external model execution after an ambiguous dispatch.

The assignment's existing monitor classifier and ten-minute polling interval
are supplied context, not a verified in-tree integration contract. Its owner,
entrypoint, and bounded cause evidence must be identified before implementation
of Task 02. Do not silently equate ACP Monitor tools with this classifier or
introduce a paid classification model. B remains conditional on that integration;
the package is complete for owner review, not implementation-ready by default.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| REQ-TASKS-CHILD-STALL-001 | Producer and classification; delivery boundary |
| REQ-TASKS-CHILD-STALL-002 | Persistence and recovery; delivery boundary |
| REQ-TASKS-CHILD-STALL-003 | Visibility and retention |

## Producer and classification

Propose internal `child.turn.settled` as a notification that a durable candidate
exists. Payload: schema version, opaque child/parent/workspace/session/turn IDs,
start transition ID, settlement time, and bounded cause hint. No message text,
credentials, arbitrary target session, or caller-chosen authority is included.
Use internal service wiring; no public webhook or new plugin permission.

Capture parent/workspace and the current task transition ID when the turn is
created, consistently with the task's entry. `ReserveTurn` is not proof that
execution began: record live dispatch acceptance separately. An explicit
settlement discriminator distinguishes live end, abandonment, cancellation,
and rejected dispatch. Legacy turns without this evidence are ineligible;
never reconstruct live provenance from `had_output` or equal timestamps.
An unavailable start snapshot must not obstruct the existing child turn; record
unknown eligibility and expose it, without inventing an entry later.

Persist the live-end discriminator and candidate in the completion transaction.
The producer's explicit live path must account for the orchestrator settling
execution state and applying pending completion transitions before the consumer
can qualify it. Existing `turn.completed` semantics stay unchanged.

The monitor reads authoritative state and classifies structured evidence:

| Cause | Required evidence | Suppression |
| --- | --- | --- |
| `input_required` | Unresolved question or explicit required-input state for this turn | Already answered/resolved input |
| `execution_error` / `quota` | Persisted terminal runtime cause for this execution | Cancellation or unrelated historical error |
| `missing_completion_signal` | This turn required the signal and no accepted signal exists after completion handling settled | Optional signal, ordinary success, or pending transition processing |

Cause precedence for a single item is input, quota/error, then missing signal;
multiple causes do not create multiple receipts. Do not use no-output, elapsed
time, or the observed 5.8% rate as classification. If the existing monitor needs
unstructured interpretation, return an explicit unresolved integration decision
rather than adding inference costs to the approved design.

No workflow entry (including a pre-ledger legacy task) is unknown, not entry
zero. A candidate with an unknown identity is retained as a visible diagnostic
and cannot deliver. Current-state reads that fail remain retryable. A known
state change makes the old candidate suppressed, even if a later turn stops
again; the later turn gets its own identity.

## Persistence and recovery

Propose a task-owned compact `child_turn_stall_receipts` table keyed by
`(child_task_id, session_id, turn_id, step_transition_id_at_turn_start)` with
captured parent/workspace, state, cause, attempt count, next attempt, lease
expiry, settlement time, queue item ID, and bounded last-error code. Use a
unique composite constraint, not a hash. Unknown provenance diagnostics remain
on the existing turn metadata until identity is available or explicitly unknown.

States: `pending -> claimed -> suppressed | ready -> queued -> delivered`;
transient failure returns to pending/ready, exhausted attempts become `failed`.
Queue acceptance means queued; delivered means the normal queue consumer's
acknowledgement, not merely bus publication. Claims are expiring compare-and-swap
leases so restart or two consumers cannot lose or duplicate work.

The table is also the durable outbox; bus events only accelerate its drain.
Recover due rows at startup and on a bounded reconciliation interval. Proposed
internal defaults for review: 30-second fallback scan; five attempts with waits
of 1, 5, 30, and 120 seconds; 60-second renewable claim lease. Failed rows retain
their original event metadata and remain inspectable for explicit retry. These
are proposed implementation constants, not settings changed by this task.

Do not atomically mark delivered before inserting the parent item. The queue
admission seam must accept the receipt's stable operation identity and return
the existing queue/history identity on replay, including after queue consumption.
Use shared transaction support if available, otherwise durable keyed admission
with readback after ambiguous insertion. Extending this seam is in Task 03;
an ordinary enqueue followed by a receipt update is insufficient.

## Delivery boundary

Immediately before keyed queue admission, re-read child relationship/workspace,
archive/terminal status, settling session's live execution/open turn, workflow
entry, and cause evidence. Serialize the check and insertion with task/session
admission and the relevant relationship/transition writers, or use an equivalent
repository conditional write. A check followed by an unlocked insert is not
sufficient. A transition committed first suppresses the alert; a transition
after admission cannot retroactively invalidate an already observed history item.

Resolve only the parent's current primary session. Missing primary session is
retryable, never a reason to pick a sibling or create a session. Preserve
Auto-run OFF, pending questions, WIP, cancellation, queue capacity, FIFO,
session incarnation, and normal prompt admission. Queue-full retries through
the receipt; no interrupt, priority bypass, or duplicate ordinary child report.
Before a queued alert is dispatched, recheck eligibility to suppress a stale
offline alert without creating a model turn. Existing managed-input question
delivery must not be duplicated by a second prompt; link its existing delivery
identity where it already represents this same question, otherwise hold the
candidate with an explicit diagnostic until the integration can preserve this
rule. Do not answer or resolve that question.

## Visibility and retention

Reuse the parent conversation and queued-message surfaces for the attributed
system item. Metadata supplies child link, step, cause, turn reference, and
receipt status; UI copy must use localization. No new dashboard is proposed.
Operators inspect pre-queue/failed receipts and retry through a narrowly scoped
task diagnostic read/retry operation using existing workspace authorization;
the route binding is part of Task 03, not a public arbitrary recipient API.
Expose pending count, oldest pending age, and bounded failure reason in existing
diagnostics/logging. Task/session identifiers are not metric labels.

The content-only phone path uses the existing dedicated mobile task conversation
(`apps/web/components/task/task-layout.tsx` and `components/task/mobile/`).
Keep its single conversation scroll owner and navigation; child links are
keyboard accessible and have coarse-pointer touch targets. No new overlay or
desktop layout compressed into a phone is needed. See the plan's UI-01 preview.

Proposed resolved receipt retention is seven days. Before purging a resolved
receipt, persist a compact resolved marker on its source turn; an old event
must match that source and marker before it can be claimed. Missing/deleted
source turns cannot create candidates. This bounds receipt history without
turning expiry into permission to duplicate. Do not purge pending/failed rows
on that timer; retain them until resolution or authorized task deletion. Include
the new table in task/workspace cleanup and apply both supported SQL dialects.
No transcript payload is copied into the receipt.

## Decision status and compatibility

See the [owner comparison](../../../plans/child-turn-stalled-signal/plan.md#owner-decision).
B is the recommendation; no accepted ADR is created for an unmade owner choice.
The A path would still require the eligibility and durable admission contracts;
only classification placement changes. Existing terminal-child waves,
notification-provider events, and draft peer-report batching stay independent.
