---
id: "02-operational-reclamation-and-readback"
title: "Document and read back the one-time backlog reclamation"
status: pending
wave: 2
depends_on:
  - 01-completed-stream-prefix-retirement
plan: "plan.md"
requirements:
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003
acceptance_criteria:
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003.1
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003.2
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003.3
system_design:
  - ../../specs/platform/system-design/delivery-completed-stream-retirement.md
---

# Task 02: Document and read back the one-time backlog reclamation

## Summary

Document the exact row conditions and the operator procedure for reclaiming the already accumulated backlog with the existing offline maintenance entry point, and record the readback. This work order changes documentation only; the operator runs the command.

## In scope

- The exact SQL conditions under which a row is deleted (below), matching the production retention code.
- The rows that are kept.
- The exclusive-access requirement, the execute/re-run/restore order, and the read-only before/after accounting.
- A read-only estimate of deletable rows counted on the approved operational database copy (path requested from the parent; recorded when provided).

## Out of scope

- Executing any `DELETE`/`UPDATE`/`VACUUM`/`PRAGMA`/maintenance command against the production database from this work order.
- The `KANDEV_TASK_DELIVERY_RETENTION_REPLAY_AGE=1h` override as a substitute for the code fix.
- New standing monitors, ledgers, dashboards, gates, or a new cleanup framework/verifier.

## Deleted rows (the contiguous projected prefix)

A row of `agent_delivery_inbox` is deleted only when every condition holds, then only as the contiguous run `sequence = retired_sequence + 1 ... nextRetired` for its stream:

- Cursor identity matches the row: `cursor.session_id = row.session_id AND cursor.incarnation_id = row.incarnation_id AND cursor.harness_generation = row.harness_generation`.
- `row.sequence <= cursor.projected_sequence` and the row is projected (`row.projected_at IS NOT NULL`).
- `row.event_type` is a known type (message_chunk, reasoning, tool_call, tool_update, plan, agent_plan, the projected session-state types, or complete/error/cancelled/canceled).
- Retained-submission proof: `row.submission_id = '' OR EXISTS (agent_delivery_submissions s WHERE s.id = row.submission_id AND s.session_id/incarnation_id/harness_generation match AND s.state IN ('prepared','dispatching','accepted','interrupted_unknown','completed','failed','cancelled'))`.
- No open live recovery block for the incarnation (`session_recovery_blocks` open AND owner session not COMPLETED/FAILED/CANCELLED AND not superseded by a newer primary).
- Not at or after the first unsettled terminal: `first_unsettled_terminal IS NULL OR row.sequence < first_unsettled_terminal`.
- `row.payload_pruned_at IS NOT NULL` (payload already compacted after the one-hour grace), `row.hash_version = 1 AND row.content_hash <> ''`.
- Completed generic receipt: a completed `agent_delivery_effects` row with `effect_key = 'agent_delivery.event:' || stream_id || ':' || sequence`, `effect_type = 'agent_delivery.event'`, `state = 'completed'`.
- No pending and no unknown effect for the row.
- Expiry: `row.projected_at <= now() - replayAge` OR `row.sequence <= cursor.projected_sequence - replayEvents` OR the new completed-stream condition: `cursor.received_sequence = cursor.projected_sequence` AND a fully settled terminal exists for the stream (settled submission with matching terminal stream/sequence/turn, completed terminal-settlement and workflow effects) AND no submission of the incarnation is in `prepared/dispatching/accepted/interrupted_unknown`.

The matching completed generic `agent_delivery_effects` receipt is deleted in the same transaction as its inbox row.

## Kept rows

- `agent_delivery_cursors` for the stream (`retired_sequence` advances; never reset to zero; `projected_sequence`/`received_sequence` unchanged).
- `agent_delivery_submissions` identity, payload hash, state, outcome, times and terminal binding.
- `agent_delivery_effects` terminal-settlement and `workflow.on_turn_complete` receipts (only the per-event generic receipt is retired with its row).
- Canonical `task_session_messages`, turns, tool history and all user-visible history.
- Any unprojected, unknown-type, protected, oversized, pending/unknown-effect row and its generic receipt; contiguity stops there.

## Operator procedure (documentation only)

1. Stop the Kandev service: `--execute` requires exclusive access to the database (`kandev must not be running against the same home/database`).
2. `kandev maintenance database --execute --compact`. It always takes and verifies a fresh backup under `<database-dir>/backups/kandev-pre-maintenance-<timestamp>.db`, deletes in bounded transactions (a capped/partial run is rerun to continue), and stages a verified `VACUUM INTO` compacted copy, installed atomically with `<database-path>.pre-compact-<timestamp>.bak` for manual rollback.
3. Restart the replaced binary and read back one hour of natural inflow/projection/terminal settlement, plus `agent_delivery_inbox`/`agent_delivery_effects` row counts and projected/terminal state.
4. Rollback: no schema/wire/journal format change, so a pre-change binary operates on the pruned database; for physical rollback move the `.pre-compact-<timestamp>.bak` file back over the live path after stopping Kandev.

The command applies the same retention batch as the background worker, so its delete conditions are exactly those above; no new framework, verifier, hash or ledger is created.

## Read-only copy estimate

Counted on the approved operational database copy (path requested from the parent), reading only table/event_type/row-count/status metadata:

- Copy path and copy timestamp: pending parent-provided path.
- Aggregation scope and counted deletable rows: pending.

The 2.29M total inbox row count is not a measured deletable figure and is not used as one.

## Verification

- `python3 scripts/list-docs.py validate`
- `python3 scripts/lint-spec-files.py --all`
- Operator readback commands and observed row counts recorded here when run.

## Dependencies and risks

- Depends on Task 01 shipping in the replaced binary.
- Risk: the estimate requires the operational copy; until the path is provided, the source-derived conditions stand and the number stays explicitly pending.
- Risk: physical shrinkage depends on the offline compaction step, not on logical deletion alone.
