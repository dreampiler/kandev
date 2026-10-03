---
status: draft
system: tasks
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-DELIVERY-001
---

# Peer Report Batch Delivery System Design

## Purpose and boundaries

The queue currently reserves one FIFO head in
`messagequeue.Service.ReserveQueuedWithAutoRunForSession`, then
`orchestrator.Service.drainQueuedMessageForPromptableSessionLockedWithTaskAdmissionAndIdentity`
dispatches that one row. Admission-time Auto-merge cannot combine reports from
different sending tasks because the merged row would lose provenance. The new
delivery path groups already-admitted rows without changing their admission
identity, storage format, queue cap, or UI merge policy.

The owner explicitly allowed choosing between delivery batching and an
operator-admission exception. Delivery batching preserves FIFO order and does
not require a second priority queue or a user-over-agent ordering rule. It
clears a burst once the receiving session becomes promptable; it cannot prevent
`queue_full` during an indefinitely busy turn. Reserving an operator slot
would shift overflow to child reports or require a new priority and capacity
contract, so it is outside this package.

## Requirement mapping

| Criteria | Design sections |
| --- | --- |
| `AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.1` through `.4` | Batch eligibility and composition |
| `AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.2`, `.5` | Delivery and recovery |
| `AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.6` | Admission guards and observability |

## Batch eligibility and composition

Extend the queue repository and service with an exact-session-incarnation
operation that reserves the FIFO head and, only when it is an ordinary peer
report, the longest eligible consecutive prefix within ten rows and 64 KiB of
report bodies. The selection and reservation are one atomic queue mutation
under the existing task-before-session admission locks. Both the SQLite and
memory repositories implement the same rule. Keep the current one-row
reservation for a non-peer head.

Each source must have `queued_by=agent`, a non-empty trusted
`sender_task_id`, no durable lifecycle or managed-input marker, no pending
question or interrupt semantics, no attachment or entity-reference payload,
and the same target task/session, model, and plan mode as the head. Stop at the
first incompatible row, including a user instruction. Never search past it.
An oversized first report uses the existing single-row path. This keeps
special metadata and provider-specific delivery out of the batch contract.

Compose one prompt from the reserved rows in FIFO order. A server-generated
header for each segment gives its ordinal and the recorded sender task title
and ID; its original body follows unchanged. Titles are display context, not
authorization. The batch envelope is distinct from user-authored text, and
the batch does not rewrite any source row or fold sender metadata together.

## Delivery and recovery

The guarded orchestrator drain passes the reserved group to a batch dispatch
path. It performs the existing task admission, session-incarnation, WIP,
clarification, cancellation, edit-lease, and in-flight checks before side
effects. One prompt turn receives the composed input; `on_turn_start` runs
once. Record each source as a separate transcript message with its own
`sender_task_id`, sender title, queue ID, and the same receiving turn identity
using the existing idempotent queued-message recording seam. Do not create a
second synthetic transcript row containing the combined body.

A durable group claim identifies the reserved source IDs in order. Acknowledge
all of them only after the prompt is accepted. If dispatch fails before
acceptance, release or restore the group in original order and retain
per-source recorded markers so a retry cannot duplicate transcript rows.
After process restart, the existing reservation recovery must either resume
the same group safely or restore its unaccepted rows; it must not treat one
member as independently delivered. Session replacement invalidates the whole
group. Keep the non-batch dispatch path for passthrough and all excluded rows.

## Admission guards and observability

Auto-run OFF and current task/session workflow barriers prevent reservation.
The existing `message.queue.status_changed` event reports rows reserved,
acknowledged, or restored. Add a bounded structured log with batch count and
outcome, excluding message bodies and task/session IDs from metric labels.
Queue status and transcript continue to show separate source messages. No
settings, WebSocket action, API field, or frontend control is introduced.

## Verification

Focused messagequeue tests cover mixed senders, FIFO boundaries, bounds,
incarnation mismatch, concurrent reservation, rollback, and both repository
implementations. Focused orchestrator tests cover one turn for multiple
reports, separate sender-attributed transcript entries, unchanged user-row
order, Auto-run OFF, dispatch failure, and restart recovery. Run only the
changed backend packages with `go test -p 2 -tags fts5 -run`; tests that open
listeners, the full Go suite, and E2E are excluded by the owner request.
