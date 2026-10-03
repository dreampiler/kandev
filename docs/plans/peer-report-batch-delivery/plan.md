---
created: 2026-10-01
status: draft
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-DELIVERY-001
system_design:
  - ../../specs/tasks/system-design/peer-report-batch-delivery.md
legacy_specs: []
---

# Implementation Plan: Peer Report Batch Delivery

## Overview

The reported MPM controller reached its ten-row queue limit with nine child
reports. The backend currently reserves and dispatches one queued row per
turn; admission-time Auto-merge cannot combine reports from different child
tasks. Add a bounded, atomic FIFO reservation for adjacent peer reports, then
deliver that group in one turn with individual sender-attributed transcript
entries. Build the reservation before changing dispatch so partial failures
cannot lose or reorder reports.

## Scope

### In scope

- Text-only ordinary agent reports from distinct child tasks, when they are a
  consecutive FIFO prefix and Auto-run is enabled.
- One receiving agent turn, original report order and bodies, distinct sender
  attribution, exact-session ownership, and group failure recovery.
- Focused Go tests in the changed packages.

### Out of scope

- Raising the per-session queue cap, reserving or reordering an operator slot,
  changing the same-sender admission Auto-merge policy, or adding a setting.
- User/workflow/lifecycle/clarification/managed-input rows, attachments,
  passthrough dispatch, frontend changes, and operating Kandev replacement.
- Listener-opening tests, the full Go suite, and E2E.

## Technical approach

`messagequeue.Repository` gains an atomic exact-incarnation reservation for
the eligible FIFO prefix. `repository_sqlite.go` and `repository_memory.go`
apply the same stop-at-first-incompatible rule and preserve the existing
single-head path for all other messages. A durable group claim tracks source
IDs until one acknowledgement or ordered release.

The guarded drain in `event_handlers_workflow.go` passes a group to
`event_handlers_agent.go`. Its dispatch composes sender-labeled segments for
one prompt, records each original queue source separately through the
idempotent transcript seam, and acknowledges the group only after prompt
acceptance. The existing session identity, clarification, edit, WIP, and
in-flight guards continue to apply. A failed or ambiguous prompt keeps the
group retryable without duplicate visible messages.

## Tests

| Acceptance | Focused evidence |
| --- | --- |
| `.1`, `.3`, `.4` | Queue repository tests for a mixed-sender prefix, user-row boundary, incompatible metadata, size/count limits, and solo fallback. |
| `.5`, `.6` | Queue tests for Auto-run OFF, exact incarnation, concurrent reservation, restoration, and restart. |
| `.1`, `.2`, `.5` | Orchestrator tests for one turn with separate sender-attributed transcript rows, ordered prompt segments, accepted acknowledgement, and failed dispatch. |

Use existing test files for these focused cases. The precise test names in the
work orders are the intended regression cases; implementation may adjust their
names while preserving the narrow `-run` command and acceptance mapping.

## Work orders

- [ ] [Task 01: Reserve peer report batches atomically](task-01-reserve-peer-report-batches.md)
- [ ] [Task 02: Dispatch a peer report batch as one turn](task-02-dispatch-peer-report-batches.md)

Task 02 depends on Task 01. No implementation work is parallelized.

## Verification results

Pending implementation. The owner limited tests to changed Go packages with
`go test -p 2 -tags fts5 -run`; do not run listener-opening tests, the full
suite, or E2E. Operating replacement belongs to the operator after a build.

## Risks

- The current transcript and prompt hooks assume one queue row per turn.
  Implement group recording without creating a duplicate combined message.
- A process crash between transcript recording and prompt acceptance needs a
  retry-safe group claim and idempotent per-source recording.
- Delivery batching clears reports only when a session becomes promptable; a
  long-running turn can still reach `queue_full` before that boundary.
