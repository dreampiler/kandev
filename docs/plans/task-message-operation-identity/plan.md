---
created: 2026-10-04
status: draft
requirements:
  - REQ-PLATFORM-TASK-MSG-OPID-001
  - REQ-PLATFORM-TASK-MSG-OPID-002
  - REQ-PLATFORM-TASK-MSG-OPID-003
  - REQ-PLATFORM-TASK-MSG-OPID-004
system_design:
  - ../../specs/platform/system-design/task-message-operation-identity.md
legacy_specs: []
---

# Implementation Plan: Task message operation identity

## Overview

Make an agent-to-agent send identifiable and its outcome readable, so a caller
whose `message_task_kandev` call timed out can learn what happened instead of
retrying blind and duplicating the message.

The change is one optional request parameter, one durable operation record
claimed before the first delivery side effect, a receipt in the success
response, one readback tool, bounded retention, and one closed-set metric. It
is backend-only and fits one PR.

## Scope

### In scope

The operation identity on `message_task_kandev`; the durable operation
record; claim-before-dispatch ordering; the response receipt fields; the
readback tool; bounded retention; the closed-set metric; and the backend tests
that pin each acceptance criterion.

### Out of scope

Queue ordering and admission, delivery-mode semantics, the launch budget, the
user-composer send path, cross-workspace messaging policy, frontend UI, and any
change to `get_task_conversation_kandev`.

## Technical approach

Persist an identity before dispatch so the outcome is readable; keep every
existing send path byte-identical when no identity is supplied; and reuse
artifacts the code already produces but currently discards, namely
`*models.Message.ID` from the immediate path and `queuedEntryID` from the
queue path.

| Boundary | Change | Evidence |
| --- | --- | --- |
| Identified send | `operation_id` parameter, claim before dispatch, response receipt | First-claim test asserts the row exists before the dispatch seam runs; no-identity test asserts today's behavior is unchanged |
| Determinable outcome | Readback tool returning state and `retry_safe` | Readback tests for committed, pending, failed, and unknown |
| Once-only replay | Unique-violation replay, direct request-field guard, no re-interrupt | Replay test asserts message count, queue length, and turn count all unchanged; interrupt-replay test asserts no cancel |
| Bounded ownership | Sender-scoped readback, batched retention, closed-set metric | Cross-sender denial test, retention batch test, metric label test |

### Request comparison

A replay is matched to its claimed operation by comparing the stored
delivery-determining request fields, prompt, delivery mode, and requested
target session, directly against the replayed values. No digest, hash,
checksum, or signature is computed, stored, or compared. The comparison
answers only whether this is the same send; it is not a content-integrity or
tamper-detection control.

## Delivery order

| Order | Work order | Result |
| --- | --- | --- |
| 1 | [01 Persist the design package](task-01-persist-design-package.md) | The durable documents exist as repository files. |
| 2 | [02 Operation record, repository, migration](task-02-operation-record.md) | A durable, restart-surviving record with a unique identity constraint. |
| 3 | [03 Claim, replay, receipt](task-03-claim-replay-receipt.md) | Identified sends claim before dispatch and replay as a read. |
| 4 | [04 Readback, retention, observability](task-04-readback-retention-observability.md) | The caller can resolve an ambiguous send. |
| 5 | [05 Integration verification](task-05-integration-verification.md) | The changed scope runs for real and identity-free sends are unregressed. |

Tasks 01 and 02 may proceed in parallel. Task 03 follows 02. Task 04 follows
03 and shares the tool-registration site with it, so those two edits are
sequenced rather than parallel.

## Risks

| Risk | Mitigation |
| --- | --- |
| Claim row written, delivery never attempted, because the process dies in the gap | Readback reports `pending`, documented as do-not-retry. An age sweep marks very old `pending` rows `failed` with `outcome_unknown` so they do not pin forever. |
| Duplicate delivery if a caller retries under a new identity | Out of contract by design; AC-001.2 preserves today's semantics. The tool description must tell callers to reuse the identity on retry. |
| Two concurrent first claims for one identity | The unique index decides and the loser takes the replay path. No lock beyond the index is needed. |
| Schema and ownership ceremony is easy to under-do | `requiredstores`, `storeconformance`, and `sqlguard` are explicit Task 02 acceptance items. |
| The message-task handler file is already at the file-size limit | All new logic goes in new files; the handler gains only the parameter and the single seam call. |

## Splitting

One PR. If it proves too large, split after Task 02, which is independently
reviewable and changes no behavior on its own.

## Verification strategy

Each work order owns its own verification at the narrowest sufficient
boundary. Task 05 runs the full backend suite as the integration proof that
identity-free sends are untouched. No end-to-end browser evidence is required:
no acceptance criterion describes rendered UI.

## Documentation synchronization

On landing, update the backend `AGENTS.md` MCP subsection only if a new durable
rule emerged, for example a new entry point that must authorize. The root
`AGENTS.md` observability list gains one bullet for the new metric. There is no
public-documentation impact; the tool is agent-facing.