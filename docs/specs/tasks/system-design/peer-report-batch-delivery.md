---
status: current
system: tasks
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-001
---

# Peer Report Batch Delivery System Design

## Boundary and mapping

The task system owns the controller prompt queue. This design extends the
existing drain path in `internal/orchestrator` and the queue service; it adds no
new persistence and no new admission rules.

| Criteria (AC-TASKS-PEER-REPORT-BATCH-001) | Sections |
| --- | --- |
| .1, .2, .4 | Batch reservation and composition |
| .3 | Eligibility boundary |
| .5, .6 | Settlement |
| .7 | Drain integration |
| .8 | Known limits |

## Batch reservation

`messagequeue.Service.ReservePeerReportBatchWithAutoRunForSession` mirrors
`ReserveQueuedWithAutoRunForSession`: it runs under `WithSessionAdmission`, it
returns the same `autoRun` meaning (false only when Auto-run is OFF), and it
reserves the head exactly as the single-row reserve does.

The reservation walks the session's ordered pending rows by repeatedly calling
the existing `ReserveHeadIfAutoRunForSession`, so both the in-memory mutex and
the SQLite session lock and write transaction already in place keep ordering and
staleness handling. Reserved rows are not removed from the queue and no new
queue column is introduced.

The walk stops at the first row that cannot join the batch. That row is returned
to its own position with `RequeueAtHeadForSession` so the next drain settles it
in its own turn. A durable row keeps its queue position while its reservation is
in flight, so reserving it again returns the same entry; the walk stops on that
repeat instead of releasing a live reservation.

## Eligibility boundary

`IsOrdinaryPeerReport` is the single definition of an eligible row, shared by
reservation and tests. A row is eligible when it was queued by an agent, carries
a non-empty sender task id, is not managed input, is not a durable delivery row,
is not already reserved in flight, has no attachments, and carries no entity
references or context files that must stay separable. A non-eligible head is
delivered alone through the unchanged single-row path.

A joining report must also agree with the leading entry on model and plan mode,
so one batch is one dispatch shape.

## Bounds

`MaxPeerReportBatchEntries` (8) and `MaxPeerReportBatchBytes` are named
constants in `messagequeue`, not configuration. Reports beyond either bound stay
queued in order.

## Composition

`ComposePeerReportBatch` renders each report with its index and sender task id,
in queue order, into the leading entry's content. The composed entry keeps the
leading entry's identity, model, plan mode, and reservation bookkeeping, so the
existing dispatch, in-flight marking, and lifecycle claim paths apply unchanged.

## Settlement

On success the composed entry is dispatched through the same in-flight marking
and asynchronous execution helpers the single-row path uses, so the underlying
rows settle as they do today.

If the batch cannot start (replaced session incarnation, or an unreadable
dispatch-input check), `requeuePeerReportBatch` returns every reserved entry from
the last one to the first. Requeue-at-position restores each row ahead of later
rows, so no report is lost and FIFO order is preserved.

## Drain integration

Both `drainQueuedMessageForPromptableSessionLockedForIdentity` and
`drainQueuedMessageForPromptableSessionLockedWithTaskAdmissionAndIdentity` call
one helper, `reserveAndDispatchQueueHead`, which reserves a batch and dispatches
it. A single-row result takes the unchanged `dispatchTakenQueuedMessageForSession`
path, so non-batchable heads behave exactly as before.

## Known limits

Batching does not remove `queue_full`. While one controller turn is long-running
and more reports arrive than a single bounded turn can absorb, admission still
rejects the surplus. Batching reduces how quickly a fan-out burst fills the
queue; it does not make exhaustion impossible, and no requirement claims
otherwise.