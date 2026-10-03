---
id: "02-dispatch-peer-report-batches"
title: "Dispatch a peer report batch as one turn"
status: pending
wave: 2
depends_on:
  - "01-reserve-peer-report-batches"
plan: "plan.md"
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-DELIVERY-001
acceptance_criteria:
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.1
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.2
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.3
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.5
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.6
system_design:
  - ../../specs/tasks/system-design/peer-report-batch-delivery.md
---

# Task 02: Dispatch a Peer Report Batch as One Turn

## Summary

Connect the group reservation to the guarded orchestrator drain. Dispatch one
sender-labeled prompt and keep each source report separately attributable in
the receiving transcript.

## In scope

- Update `internal/orchestrator/event_handlers_workflow.go` to use the group
  reservation at the ordinary Auto-run drain boundary.
- Update `internal/orchestrator/event_handlers_agent.go` and the existing
  `promptTask` callback seam to compose one bounded prompt, record each source
  idempotently with its sender metadata, and settle the entire group after
  prompt acceptance or failure.
- Add focused cases to existing orchestrator test files for one turn, mixed
  sender attribution, order, user-row boundary, Auto-run OFF, and failed or
  interrupted delivery.

## Exclusions

- No frontend or public API change, passthrough batch, special-row batch,
  operator priority, or operating-instance replacement.

## Implementation acceptance

1. Multiple eligible reports produce one agent turn with FIFO body order and
   separate transcript rows retaining each sender task.
2. A user or special row stops the batch and remains in place for its normal
   turn; existing workflow and clarification barriers still apply.
3. On failed dispatch or restart, unaccepted reports remain retryable without
   loss, reorder, or duplicate transcript rows.

## Verification

From `apps/backend`, run:

```text
go test -p 2 -tags fts5 -run '^TestPeerReportBatch' ./internal/orchestrator
go test -p 2 -tags fts5 -run '^TestPeerReportBatch' ./internal/orchestrator/messagequeue
```

Do not run listener-opening tests, the full Go suite, or E2E.

## Dependencies and risks

Task 01 must provide the durable group claim. The current `promptTask` and
queued transcript hooks are row-oriented. Preserve the single-row path for
excluded messages and ensure a combined dispatch does not create a second
synthetic chat row that hides individual sender attribution.

## Results

Pending.
