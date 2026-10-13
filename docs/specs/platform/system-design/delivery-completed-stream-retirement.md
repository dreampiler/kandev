---
status: draft
system: platform
requirements:
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003
---

# Completed-stream delivery retirement system design

## Boundary and evidence

Platform owns retained delivery evidence; Tasks owns the schema; the lifecycle authenticates traffic; the orchestrator owns queue and workflow authority.
This design extends [backend agent delivery retention](backend-agent-delivery-retention.md) and its [checkpoint ADR](../../../decisions/2026-10-05-backend-delivery-retirement-checkpoints.md).
It changes no wire contract, journal schema, table, column or index.
The requirement owner is [here](../requirements/delivery-completed-stream-retirement.md).

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001 | Root cause; completed-stream predicate; eligibility |
| REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002 | Replay and recovery safety |
| REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003 | Bounded work; operations; compatibility |

## Root cause (source-confirmed)

The current retention bounds payload bytes but keeps rows for the whole per-stream replay window.

- The retirement head query admits a stream only when its head row is compacted and the head is already outside the window: `head.sequence <= projected_sequence - replayEvents` or `head.projected_at <= ageCutoff`, where `ageCutoff = now - ReplayAge` (`agent_delivery_retention_discovery.go:117-123`).
- The same gate is re-checked on the locked candidate: `ageExpired || countExpired` with `ageCutoff = now - replayAge` (`agent_delivery_retention_events.go:105-119`).
- Defaults are `ReplayAge = 24h` and `ReplayEvents = 10000` (`config/source.go:400-408`, `config/catalog.go:119-120`).
- Payload compaction empties the fragment bytes after the one-hour grace but does not delete the row, so rows, their indexes and their generic receipts keep growing until the window expires (`agent_delivery_retention_events.go:204-241`).
- Retirement is a contiguous projected prefix; one unclassified or protected row stops the stream's prefix (`agent_delivery_retention_discovery.go:94-105`, `agent_delivery_retention_event_types.go:9-10`).

Consequence: a completed stream whose event count is below `ReplayEvents` is bounded only by `ReplayAge`; the inbox therefore retains roughly a full replay window of live inflow.
With high-cardinality reasoning and message-chunk fragments, that window is millions of rows and multiple gigabytes even after payload compaction.

The same gate already treats an expired replay as recoverable: `loadAgentDeliveryEventTx`/`validateAgentDeliveryCursorEvent` return `ErrAgentDeliveryReplayExpired` for `sequence <= retired_sequence` (`agent_delivery.go:320-322`, `:636-641`), and the lifecycle reconciles the projected cursor instead of applying the event (`agent/runtime/lifecycle/streams.go:674-683`). Retirement metadata (checkpoint, submission, terminal, workflow) already survives independently of the fragment row (`agent_delivery_retention_proof.go:9-57`).
A completed stream therefore has no consumer that needs its fragment rows, which is why widening the gate to completed streams is safe while adding no new evidence.

## Completed-stream predicate

The locked candidate already exposes `projected_sequence`, `retired_sequence`, cursor identity and effect evidence (`agent_delivery_retention_candidates.go:67-159`). Extend the eligibility test so a stream qualifies when all hold:

1. `cursor_identity_valid` and `sequence <= projected_sequence`.
2. `projected_sequence == received_sequence` (fully received and projected) and no inbox row at `projected_sequence + 1` (no unprojected gap), read under the existing stream lock.
3. Either the retained terminal-submission proof holds for the stream's terminal, or the stream's incarnation has no unresolved terminal and no active submission.
4. No open recovery block for the live incarnation (`agent_delivery_retention_proof.go:84-89`), no pending effect, no unknown effect.
5. `payload_pruned_at IS NOT NULL` (compact first, delete later) and the contiguous prefix from `retired_sequence + 1` is intact.

When these hold, `agentDeliveryRetentionSequenceExpired` treats the row as expired irrespective of `ReplayAge`/`ReplayEvents`.
Non-completed streams keep the current window unchanged, so replay and diagnostics for live or uncertain streams are preserved.

Eligibility stays a strict superset of the current predicate: no new deletion reach, only earlier retirement of rows the window would retire later anyway.

## Replay and recovery safety

- Expired replay path is unchanged: a replay at or below the retirement checkpoint reconciles from the authenticated projected cursor and does not apply (`streams.go:674-683`).
- Cursors never reset; `retired_sequence` advances only by the exact contiguous prefix, guarded by `WHERE retired_sequence = ?` (`agent_delivery_retention_events.go:72-85`).
- Checkpoint, submission, terminal-settlement and workflow receipts are outside the retired prefix and remain (`agent_delivery_retention_events.go:133-172`, `agent_delivery_retention_proof.go:9-57`).
- Resume and `continue_from_history` read only unprojected rows; canonical messages, turns and tool history are untouched.

## Bounded work, operations and compatibility

- No new scheduler, gate, monitor, ledger or format. The existing worker keeps a 50-row background batch, one-minute idle cadence with productive catch-up, 4 MiB payload budget, 250 ms batch and 5 s pass bounds (`deliveryretention/worker.go:23-39`, `:321-373`).
- Backlog reclamation uses the existing offline entry point `kandev maintenance database --execute --compact`, which requires exclusive database access, takes and verifies a backup, and stages a VACUUM INTO copy for physical reclamation (`launcher/maintenance_database.go:16-46`). The worker never runs a foreground mass delete.
- No schema/wire/journal changes, so a pre-change binary remains a valid rollback target for the same database.
- Existing aggregate snapshots (`agent_delivery.retention.snapshot`) already report retired rows and bytes; no new surface is required.

## Verification approach

- Companion backend unit tests beside the retention source assert: a completed stream retires fragments after the grace without waiting for `ReplayAge`/`ReplayEvents`; an active or uncertain stream does not; a protected/unknown row still stops the prefix; the checkpoint and terminal/workflow receipts remain.
- One-off, not standing: a disposable database copy or narrow temporary database reproduces a high-fragment completed stream and reads the before/after row and byte counts through the real retention batch entry point.
- Operational readback (owner/operator, not claimed by this change): one hour of natural inflow, projection and terminal settlement on the replaced binary, then read the same row counts and projected/terminal state; plus the one-time `--execute --compact` run for already accumulated rows.

## Risks

- Treating a stream as completed too early would retire evidence a reconnect still needs. The predicate therefore requires received == projected, no unprojected gap, and no unresolved terminal/submission/block/effect, all read under the existing stream lock.
- Throughput can still bind on SQLite; the existing worker snapshot `oldest_eligible_age_seconds`, `event_rows_retired` and `backlog_remaining` distinguish a window-bound backlog from a capacity-bound one.
- Physical file shrinkage remains an explicit offline operation; logical deletion alone reuses pages without shrinking the file.
