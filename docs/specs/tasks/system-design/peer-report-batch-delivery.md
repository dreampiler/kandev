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
staleness handling. An ordinary row leaves `queued_messages` at reserve time and
keeps its at-least-once recovery in a `queue_dispatch_claims` row, so this
capability introduces no new queue column and no new settlement mechanism.

The walk stops at the first row that cannot join the batch. That row is returned
to its own position with `RequeueAtHeadForSession` so the next drain settles it
in its own turn. A durable row keeps its queue position while its reservation is
in flight, so reserving it again returns the same entry; the walk stops on that
repeat instead of releasing a live reservation.

A reserve that stops part-way owns the rows it already took, because those rows
have left the queue. `ReservePeerReportBatchWithAutoRunForSession` therefore
returns them alongside its error, and the caller returns them to the queue rather
than dropping them with the error.

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

A batch has one prompt but N reserved rows, and only the leading row's dispatch
claim would otherwise decide delivery. Startup restores any claim that was never
accepted, so an unsettled trailing claim would re-deliver a report the controller
had already received.

Because a folded row's report is physically inside the leading row, the leading
row's claim is the only claim that must decide delivery. Once the batch passes
its incarnation and dispatch-input checks, `markFoldedPeerReportClaimsAccepted`
accepts every trailing row's claim before the worker is launched. From then on
the batch has a single settlement owner:

- If the turn is accepted, the leading row's claim is accepted by the existing
  post-dispatch path and every claim in the batch is settled.
- If the turn fails, the leading row is requeued carrying the composed content of
  the whole batch, so no report is lost, and the already-accepted trailing claims
  cannot restore the folded rows as duplicates of it.

Accepting the trailing claims before launch is deliberately earlier than the
leading row's acceptance. A crash in that window leaves the leading claim
unaccepted, so startup restores one row holding every report in the batch.

If the batch cannot start (replaced session incarnation, or an unreadable
dispatch-input check), no claim has been accepted and `requeuePeerReportBatch`
returns every reserved entry from the last one to the first. Requeue-at-position
restores each row ahead of later rows, so no report is lost and FIFO order is
preserved.

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