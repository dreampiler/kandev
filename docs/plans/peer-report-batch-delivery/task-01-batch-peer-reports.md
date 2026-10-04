---
id: "01-batch-peer-reports"
title: "Batch peer report reservation"
status: completed
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-001
acceptance_criteria:
  - AC-TASKS-PEER-REPORT-BATCH-001.1
  - AC-TASKS-PEER-REPORT-BATCH-001.3
  - AC-TASKS-PEER-REPORT-BATCH-001.4
  - AC-TASKS-PEER-REPORT-BATCH-001.5
system_design:
  - ../../specs/tasks/system-design/peer-report-batch-delivery.md
---

# Task 01: Batch peer report reservation

## Summary

Add one shared eligibility rule and a bounded FIFO batch reserve to the
`messagequeue` service, reusing the existing per-session admission critical
section.

## In scope

- `IsOrdinaryPeerReport` as the single definition of an eligible row.
- `ReservePeerReportBatchWithAutoRunForSession` walking ordered pending rows under
  the existing admission lock and the existing repository lock or transaction.
- Stop conditions: ineligible row, model or plan-mode mismatch, byte ceiling,
  entry bound, repeated durable head, empty queue, and Auto-run OFF.
- Requeue of a non-joining row to its own position, so queue accounting and
  `ErrQueueFull` behavior are unchanged.
- Named bounds for maximum entries and maximum combined bytes.
- `messagequeue` tests covering the FIFO prefix, every stop condition, memory and
  SQLite repository agreement, and multi-row all-or-nothing settlement.

## Out of scope

Prompt composition, the drain integration, and any change to admission or
capacity rules.

## Acceptance

1. A run of N eligible reports reserves N rows in order and stops at the first
   row that is not an ordinary peer report.
2. A non-joining row is back in the queue at its own position after the reserve.
3. Memory and SQLite repositories reserve the same prefix for the same queue.

## Verification

```bash
(cd apps/backend && go test -p 2 -tags fts5 ./internal/orchestrator/messagequeue/... -count=1)
```