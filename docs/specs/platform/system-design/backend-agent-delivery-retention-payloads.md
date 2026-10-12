---
status: current
system: platform
requirements:
  - REQ-PLATFORM-DELIVERY-RETENTION-001
  - REQ-PLATFORM-DELIVERY-RETENTION-002
  - REQ-PLATFORM-DELIVERY-RETENTION-003
  - REQ-PLATFORM-DELIVERY-RETENTION-005
  - REQ-PLATFORM-DELIVERY-RETENTION-006
---

# Independent canonical delivery payload reclamation

## Boundary

This clarifies raw-output eligibility in the [parent retention design](backend-agent-delivery-retention.md).
Platform owns the delivery and recovery contract. The task repository owns its proofs;
the existing worker performs automatic cleanup. Canonical conversation retention,
prompt admission, terminal settlement, wire format and identity ownership are unchanged.

| Requirement | Design sections |
| --- | --- |
| REQ-PLATFORM-DELIVERY-RETENTION-001 | Independent raw-output proof; protected evidence; superseded tool progress |
| REQ-PLATFORM-DELIVERY-RETENTION-002 | Replay and retained receipts |
| REQ-PLATFORM-DELIVERY-RETENTION-003 | Transactions and interruptions |
| REQ-PLATFORM-DELIVERY-RETENTION-005 | Discovery progress and budgets |
| REQ-PLATFORM-DELIVERY-RETENTION-006 | Upgrade and operational cleanup |

## Independent raw-output proof

Before this repair, `agent_delivery_retention_candidates.go` applied an existing-submission
probe and `first_terminal_sequence` to both `can_prune` and `can_retire`. This prevented an
unsettled historical terminal from losing its evidence, but also keeps independently
projected later output bodies. The proof for clearing a nonterminal body must be
independent from the proof for deleting its receipt and advancing a contiguous prefix.

For nonterminal `message_chunk`, `reasoning`, `tool_call`, `tool_update`, `plan` and
`agent_plan`, raw-body eligibility requires all of:

1. The retained cursor matches the event's session, incarnation and harness generation.
2. The event is projected at or below that cursor's contiguous projected sequence,
   its projection time is valid, and the configured grace has elapsed.
3. Its exact `agent_delivery.event:<stream>:<sequence>` effect has the matching stream,
   sequence and type, is completed, and has a non-null completion time. This trusted
   receipt proves that canonical projection committed. A different effect key or a
   merely completed effect on a neighboring event is insufficient.
4. No pending or unknown required effect for this event remains.
5. Typed payload decoding confirms a supported nonterminal event, matching event
   type and non-user role. Existing envelope/hash-version/byte-count checks pass.
6. The existing row and byte budgets admit the payload.

An earlier protected terminal and a missing legacy submission row are not proof that
this independently committed body is needed for projection. They continue to protect
their own authority and settlement evidence. This exception applies only to clearing
the supported nonterminal body; it does not create a submission or establish an outcome.

If any proof is missing, conflicting or ambiguous, keep the payload and expose the
existing aggregate skip/error result. Do not infer projection from a session's visible
state, task archive flag, timestamp, canonical text similarity, or a completed tool
title. Use the authenticated persisted envelope and exact committed effect.

## Protected evidence and replay

Terminal bodies, prompt bodies, permission state, unsupported event types, unprojected
output, pending effects and unknown ownership retain the parent policy. Full terminal
settlement still requires its exact submission, terminal, workflow, queue and recovery
proofs. No historical completion or workflow no-op is invented.

For newly eligible nonterminal bodies, use `agentDeliveryCompactionHash` and
`writeAgentDeliveryPayloadCompaction` to preserve the versioned normalized hash,
original byte count, envelope, projection time and `payload_pruned_at`. Retain the
generic effect and receipt whenever `can_retire` is false. Do not reconstruct a hash
from canonical output or delete a receipt merely because its body is empty.

`can_retire`, `first_terminal_sequence`, terminal proof and contiguous prefix advancement
keep their stricter dependency checks. Retirement cannot leap a protected terminal,
missing sequence or unsupported event. The existing format-1 readers already understand
compact receipts. No new format, checkpoint reset, generation reset or wire capability
is required.

An authenticated identical duplicate still returns its committed result without
projection or dispatch. Changed content or envelope fails the existing conflict guard.
State-only ACK recovery uses the retained projected cursor. Cleanup never admits a
prompt, resolves a recovery block or certifies that an external action did not occur.

The [checkpoint decision](../../../decisions/2026-10-05-backend-delivery-retirement-checkpoints.md)
and [barrier decision](../../../decisions/2026-10-10-delivery-event-retention-barriers.md)
remain authoritative. Their existing separation of raw bodies and replay evidence
supplies the boundary; this correction does not introduce a new retention authority.

## Discovery progress and budgets

Before this repair, `deliveryretention.Worker.runPass` repeated a phase only after deletion or
pruning. A protected-only page could advance the durable discovery frontier without
removing anything, then leave later eligible payloads until another minute.

Repository batch results must distinguish committed discovery advancement from mutation
counts and exhaustion/wrap. Report progress only after the transaction commits. The
worker may continue a discovery phase after forward progress even with zero deletions,
within its existing five-second pass. An exhausted phase or a completed traversal with
no further progress stops repeating; it must not rescan one protected page in a loop.
Empty, busy, deadline, rollback and readiness failures are not committed progress.

Retain 50-candidate background leases, the configured lower ceiling, 200-candidate
repository ceiling, 4 MiB payload budget, 250 ms admitted SQL deadline, 100 ms cancellable
yield, per-pool maintenance gate and fair first visit to all three phases. Use the
existing persisted compaction/receipt frontiers; do not add startup-wide counts, JSON
scans, OFFSET pagination, lifetime-sized memory or a second worker.

Production snapshots must distinguish examined/protected candidates, committed scan
progress, removed payload bytes and a bounded backlog sample. A protected-only sample
does not prove the database-wide eligible backlog is empty. Do not add identity labels
or print payloads. Any new progress fields must be structural and bounded.

Sustained inflow can exceed one five-second pass per minute. A pass that runs out of time
while its removals still outnumber protected candidates, or that commits cleanup despite a
transient batch deadline, resets backoff and starts the next pass after five seconds instead
of the normal interval. Discovery-only or protected-dominated passes keep the interval, and a
pass without committed cleanup keeps the jittered backoff. Payload discovery selects only the
event types the locked proof can compact, so session-state events do not fill bounded pages
on every frontier wrap.

## Transactions and interruptions

Recheck exact event/effect/cursor proofs after the existing sorted session and stream
barriers inside the writer transaction. Preserve the parent SQLite writer admission
and PostgreSQL lock rules. Hash, body clearing, tombstone and progress commit together.
Interruption before commit leaves the raw body and frontier unchanged; interruption
after commit reuses the compact receipt and resumes the sweep.

Concurrent projection, replay, owner replacement, session deletion and maintenance
must preserve canonical content, old terminal evidence and successor identities.
Never cache proof across batches. The candidate SELECT, locked reload and manual
maintenance preview must agree on eligibility.

## Upgrade and operational cleanup

Normal worker startup after schema readiness automatically revisits old rows using
the existing durable frontier and wrap behavior. No destructive startup migration,
operator opt-in, feature flag or TTL for unknown outcomes is introduced. Old missing
submission bookkeeping remains missing; compacting independently proved output does
not make that history resumable or authorize a retry.

The local repair first exercises the corrected shared batch methods on an isolated
copy of a verified snapshot. Record actual removed bytes, preserved canonical table
digests/counts, protected evidence and unchanged cursor bounds. Do not deploy that
stale test copy over a live database.

For the live pass, coordinate a quiet period, acquire the standard ownership lock,
take a fresh verified backup, and repeatedly call the shared bounded repository
operations until a complete traversal establishes no further eligible work at the
captured cutoff, or the explicit operator run budget expires. A protected-only page
is not completion. Every transaction retains its normal row/byte/time limits.
Keep this as a task-specific operational runner unless an implementation requirement
justifies a permanent CLI surface. Existing one-batch CLI calls do not drain a large
backlog, and repeated calls unnecessarily create many full backups.

After logical cleanup, use the existing verified offline compaction path. Check free
space for a fresh backup plus staged compacted file and keep rollback artifacts.
Verify integrity and canonical preservation, restart the compatible corrected backend,
and observe several normal cleanup intervals. Report raw-payload growth separately
from normal history, compact receipts, WAL, backups and physically reusable pages.
No claim of a total database quota or permanent zero growth follows from this repair.

## Superseded tool progress

A long shell tool emits many in-progress `tool_update` events, and each carries the whole
(bounded) output so far. Waiting for the payload grace therefore keeps O(n²) bytes per tool
call: a 2026-10-12 production burst stored 399 updates of about 277 KB for one call.

Lifecycle observes each received `tool_update` as a typed event. It remembers a projected
update whose status is progress-only (`in_progress`, `running`, `started`, `pending`) as a
hint of sequence plus receive-time versioned hash, keyed by stream and tool call. When a
later update of that tool call is projected, it queues the hints. One tracked background
drain coalesces them per tool call, so projection and acknowledgment never wait for
compaction.

The repository treats hints only as candidates. Under the normal stream barriers it
requires the later update to be the stored, projected receipt with the same envelope and
versioned hash. It reloads each candidate's metadata through the locked worker proof with
the grace replaced by this supersession: the candidate must pass `can_prune`, be an
earlier nonterminal `tool_update` of the same stream identity, have an unpruned body whose
length equals its stored byte count, and carry a persisted hash equal to the hinted hash.
That hash equality binds the observed tool call and status to the stored row, so the writer
transaction neither loads nor decodes a body. The existing payload compaction statement
clears the body; the envelope, hash, byte count and generic receipt remain.

Unprojected rows, final tool statuses, terminal and other event types keep the normal policy.
The newest update keeps its body for the grace. Live websocket delivery, canonical tool
messages and duplicate classification are unchanged. Hints are bounded and in memory; a
restart, eviction or failed compaction leaves the body to the worker. Compaction failure never
fails projection or acknowledgment.

## Delivery record

The [repair plan](../../../plans/legacy-delivery-payload-reclamation/plan.md) records
the live diagnosis, regression matrix, work order sequence and execution checkpoint.
