---
status: draft
system: platform
created: 2026-10-13
owners:
  - kandev
---

# Completed-stream delivery retirement requirements

## Overview

Platform owns authenticated durable delivery and its retained recovery evidence.
[Backend agent delivery retention](backend-agent-delivery-retention.md) bounds redundant transport payloads and checkpoints within a per-stream replay window (10,000 events or 24 hours, whichever expires first).
That window is applied to every stream, including streams that are already terminal and fully projected, so a completed stream keeps every fragment row for the whole window.

This capability adds the missing bound: a completed stream reclaims its eligible fragment rows after the payload grace instead of waiting for the replay window.
It extends the retention capability; it does not replace its payload, prompt, terminal, workflow, deletion or crash contracts.
The paired [design](../system-design/delivery-completed-stream-retirement.md) and [plan](../../../plans/delivery-completed-stream-retirement/plan.md) define delivery.

## Terminology

- **Completed stream:** A `stream_id` whose received cursor equals its projected cursor, whose inbox has no unprojected row, and whose incarnation has no unresolved submission, terminal, recovery block, or pending/unknown effect.
- **Fragment row:** A projected nonterminal inbox row (message chunk, reasoning, tool call/update, plan) plus its generic completed `agent_delivery.event` receipt.
- **Checkpoint:** The retained cursor, submission, terminal-settlement, workflow and canonical projection evidence that recovery reads.

## Requirements

### REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001: Retire completed-stream fragments after the grace

**Intent:** A stream that no consumer can replay or recover must not retain one row per streaming fragment for the whole replay window.

#### Acceptance criteria

- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001.1:** Once a stream is completed (received equals projected, no unprojected row, no unresolved submission, terminal, recovery block or pending/unknown effect), its eligible compact fragment rows shall retire after the payload grace, independent of the configured replay age and replay event count.
- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001.2:** Retirement shall remain the contiguous projected prefix that starts after the stream's retirement checkpoint and shall never cross an unprojected, protected, unknown-type or oversized row.
- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001.3:** The generic completed `agent_delivery.event` receipt shall be deleted only with its associated inbox fragment row; checkpoint, submission, terminal-settlement and workflow receipts shall remain for the session lifetime.

### REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002: Preserve replay, recovery and settlement authority

**Intent:** Early retirement must not change identities, hashes, or recovery behavior.

#### Acceptance criteria

- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002.1:** An event replayed beyond the retirement checkpoint shall continue to return the existing expired result and reconcile from the authenticated projected cursor, without inserting, projecting, resending prompts, or advancing ACK from event-derived data.
- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002.2:** Session, incarnation, harness generation, stream identity, sequence, content hash, hash version and retirement checkpoint semantics shall be unchanged; the retirement checkpoint shall never reset to zero.
- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002.3:** Resume, `continue_from_history`, block binding and queue/terminal settlement shall keep reading only unprojected rows and the retained receipts; no cleanup path shall resolve a block, release a claim, or invent an outcome.

### REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003: Bounded cleanup, operations and compatibility

**Intent:** The added bound must stay inside the existing bounded worker and operational contracts.

#### Acceptance criteria

- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003.1:** Retirement shall use the existing bounded retention batch, serialization, maintenance-admission and cancellation behavior; it shall introduce no new gate, standing monitor, ledger, or schema format.
- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003.2:** An already accumulated backlog shall be reclaimable through the existing offline maintenance entry point (`kandev maintenance database --execute --compact`), which requires exclusive database access and a verified backup; the background worker shall not run a foreground mass delete.
- **AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003.3:** Cleanup of a completed stream shall not remove user-visible canonical messages or recovery actions; a pre-change binary shall remain a supported rollback target because no wire, journal, or schema format changes.

## Relationship to existing retention requirements

This capability refines [REQ-PLATFORM-DELIVERY-RETENTION-005](backend-agent-delivery-retention.md) storage bounds for completed streams.
REQ-PLATFORM-DELIVERY-RETENTION-001 through 006 remain authoritative for payload, prompt, terminal, deletion, crash, upgrade and observability behavior; this capability adds no new evidence category and removes none.

## Out of scope

- Coalescing or re-encoding reasoning and message-chunk events at receive, journal or wire time.
- Changing the default replay window for active or uncertain streams.
- Canonical conversation/tool expiry, agentctl journal pruning, physical compaction policy, online VACUUM, or a feature flag.
- Automatic prompt resend, synthetic settlement, or bypassing a recovery guard.
