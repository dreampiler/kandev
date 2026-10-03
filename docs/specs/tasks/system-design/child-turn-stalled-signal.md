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

The owner selected option A on 2026-10-03: the producer classifies each live
settlement and enqueues qualified alerts directly to the parent's primary
session. No internal candidate feed, monitor classifier, consumer lease, or
separate receipt storage is used. The package is complete for owner review.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| REQ-TASKS-CHILD-STALL-001 | Producer classification; delivery boundary |
| REQ-TASKS-CHILD-STALL-002 | Durable admission; delivery boundary |
| REQ-TASKS-CHILD-STALL-003 | Visibility and retention |

## Producer classification

The producer classifies each live settlement at turn completion time, using
only structured evidence already available in the task system. No external
monitor, paid classifier, or unstructured transcript interpretation is used.

Capture parent/workspace and the current task transition ID when the turn is
created, consistently with the task's entry. `ReserveTurn` is not proof that
execution began: record live dispatch acceptance separately. An explicit
settlement discriminator distinguishes live end, abandonment, cancellation,
and rejected dispatch. Legacy turns without this evidence are ineligible;
never reconstruct live provenance from `had_output` or equal timestamps.
An unavailable start snapshot must not obstruct the existing child turn; record
unknown eligibility and expose it, without inventing an entry later.

Persist the live-end discriminator and classification result in the completion
transaction. The producer's explicit live path must account for the orchestrator
settling execution state and applying pending completion transitions before
classification can qualify it. Existing `turn.completed` semantics stay
unchanged.

The producer classifies structured evidence:

| Cause | Required evidence | Suppression |
| --- | --- | --- |
| `input_required` | Unresolved question or explicit required-input state for this turn | Already answered/resolved input |
| `execution_error` / `quota` | Persisted terminal runtime cause for this execution | Cancellation or unrelated historical error |
| `missing_completion_signal` | This turn required the signal and no accepted signal exists after completion handling settled | Optional signal, ordinary success, or pending transition processing |

Cause precedence for a single item is input, quota/error, then missing signal;
multiple causes do not create multiple receipts. Do not use no-output, elapsed
time, or the observed 5.8% rate as classification. If the producer cannot
obtain structured evidence for a cause, return an explicit unresolved
diagnostic rather than adding inference costs to the approved design.

No workflow entry (including a pre-ledger legacy task) is unknown, not entry
zero. A candidate with an unknown identity is retained as a visible diagnostic
and cannot deliver. Current-state reads that fail remain retryable. A known
state change makes the old candidate suppressed, even if a later turn stops
again; the later turn gets its own identity.

## Durable admission

The producer enqueues qualified alerts directly to the parent's primary
session using the existing message queue. No separate receipt table, feed, or
consumer is introduced. The queue item itself is the durable record.

Deduplication uses the stable operation identity
`(child_task_id, session_id, turn_id, step_transition_id_at_turn_start)`.
The queue admission seam must accept this identity and return the existing
queue/history identity on replay, including after queue consumption. Use
shared transaction support if available, otherwise durable keyed admission
with readback after ambiguous insertion. An ordinary enqueue followed by a
status update is insufficient.

Do not atomically mark delivered before inserting the parent item. The queue
admission seam must return the existing queue/history identity on replay.
Extending this seam is in Task 03.

Proposed internal defaults for review: five attempts with waits of 1, 5, 30,
and 120 seconds. Failed items retain their original event metadata and remain
inspectable for explicit retry. These are proposed implementation constants,
not settings changed by this task.

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
the queue; no interrupt, priority bypass, or duplicate ordinary child report.
Before a queued alert is dispatched, recheck eligibility to suppress a stale
offline alert without creating a model turn. Existing managed-input question
delivery must not be duplicated by a second prompt; link its existing delivery
identity where it already represents this same question, otherwise hold the
candidate with an explicit diagnostic until the integration can preserve this
rule. Do not answer or resolve that question.

## Visibility and retention

Reuse the parent conversation and queued-message surfaces for the attributed
system item. Metadata supplies child link, step, cause, turn reference, and
queue status; UI copy must use localization. No new dashboard is proposed.
Operators inspect pre-queue/failed items and retry through a narrowly scoped
task diagnostic read/retry operation using existing workspace authorization;
the route binding is part of Task 03, not a public arbitrary recipient API.
Expose pending count, oldest pending age, and bounded failure reason in existing
diagnostics/logging. Task/session identifiers are not metric labels.

The content-only phone path uses the existing dedicated mobile task conversation
(`apps/web/components/task/task-layout.tsx` and `components/task/mobile/`).
Keep its single conversation scroll owner and navigation; child links are
keyboard accessible and have coarse-pointer touch targets. No new overlay or
desktop layout compressed into a phone is needed. See the plan's UI-01 preview.

Proposed resolved queue-item retention is seven days. Before purging a resolved
item, persist a compact resolved marker on its source turn; an old event
must match that source and marker before it can be claimed. Missing/deleted
source turns cannot create candidates. This bounds history without turning
expiry into permission to duplicate. Do not purge pending/failed items
on that timer; retain them until resolution or authorized task deletion.
No transcript payload is copied into the queue item.

## Decision status and compatibility

The owner selected option A on 2026-10-03; its boundary and tradeoffs are
recorded in [ADR-2026-10-03-child-turn-stalled-producer-delivery](../../../decisions/2026-10-03-child-turn-stalled-producer-delivery.md).
Existing terminal-child waves,
notification-provider events, and draft peer-report batching stay independent.
