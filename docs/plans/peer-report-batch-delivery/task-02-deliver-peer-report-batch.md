---
id: "02-deliver-peer-report-batch"
title: "Deliver a peer report batch from the drain path"
status: completed
wave: 2
depends_on:
  - 01-batch-peer-reports
plan: "plan.md"
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-001
acceptance_criteria:
  - AC-TASKS-PEER-REPORT-BATCH-001.2
  - AC-TASKS-PEER-REPORT-BATCH-001.6
  - AC-TASKS-PEER-REPORT-BATCH-001.7
  - AC-TASKS-PEER-REPORT-BATCH-001.8
system_design:
  - ../../specs/tasks/system-design/peer-report-batch-delivery.md
---

# Task 02: Deliver a peer report batch from the drain path

## Summary

Compose a reserved batch into one prompt and dispatch it from both drain paths,
with an all-rows requeue when the dispatch cannot start.

## In scope

- `ComposePeerReportBatch` rendering each report with its index and sender task
  id, in queue order, into the leading entry.
- `reserveAndDispatchQueueHead` shared by both drain variants, falling through to
  the unchanged single-row dispatch for a one-row result.
- Incarnation and dispatch-input checks before the batch is handed to an agent,
  with every reserved row returned to the queue when a check fails.
- `orchestrator` tests: N reports in one prompt with per-sender attribution and N
  rows consumed, a `report, user, report` queue delivering only the first report,
  and a long-running turn leaving later rows queued.

## Out of scope

Any change to queue capacity, admission order, or the ordering of a direct
`message.add` behind queued messages.

## Acceptance

1. A drain of N same-turn peer reports produces one prompt containing all N with
   per-sender attribution and consumes N rows.
2. `report, user, report` delivers the report, then the user row on the next
   drain; the trailing report is untouched.
3. A replaced session incarnation or an unreadable dispatch-input check returns
   every reserved row in its original order.

## Verification

```bash
(cd apps/backend && go test -p 2 -tags fts5 ./internal/orchestrator/ -run "Drain|Queue|PeerReportBatch" -count=1)
```