---
id: "01-reserve-peer-report-batches"
title: "Reserve peer report batches atomically"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-DELIVERY-001
acceptance_criteria:
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.1
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.3
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.4
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.5
  - AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.6
system_design:
  - ../../specs/tasks/system-design/peer-report-batch-delivery.md
---

# Task 01: Reserve Peer Report Batches Atomically

## Summary

Select and reserve a bounded consecutive prefix of ordinary peer reports at
the FIFO head under one exact-session-incarnation queue operation. Preserve
the existing one-row behavior for every excluded head.

## In scope

- Extend `internal/orchestrator/messagequeue/repository.go` and `service.go`
  with one group reservation and group release/acknowledgement contract.
- Implement that contract for both `repository_sqlite.go` and
  `repository_memory.go`, preserving task-before-session admission locking and
  the current Auto-run and edit-lease gates.
- Add meaningful focused cases to existing messagequeue test files for mixed
  sender tasks, FIFO barriers, ten-row and 64 KiB bounds, exact incarnation,
  concurrent drains, rollback, and restart recovery.

## Exclusions

- No prompt composition, transcript change, capacity-setting change, or new
  queue type. Do not weaken same-sender Auto-merge restrictions.

## Implementation acceptance

1. One reservation returns at most the eligible contiguous peer-report prefix
   in FIFO order, and a non-peer head follows the current single-row path.
2. A group is acknowledged or restored as a unit for its exact task/session
   incarnation; another drain cannot claim one of its members concurrently.
3. Failed selection or storage leaves the original rows and Auto-run policy
   unchanged.

## Verification

From `apps/backend`, run:

```text
go test -p 2 -tags fts5 -run '^TestPeerReportBatch' ./internal/orchestrator/messagequeue
```

Do not run listener-opening tests, the full Go suite, or E2E.

## Dependencies and risks

This work order has no prerequisite. Repository reservation semantics are
shared by SQLite and PostgreSQL through existing SQL rebinding; check both
without introducing a separate PostgreSQL-only path. Recovery must retain the
ordered source IDs so Task 02 cannot acknowledge only part of a group.

## Results

Pending.
