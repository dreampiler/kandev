---
created: 2026-10-04
status: completed
requirements:
  - REQ-TASKS-PEER-REPORT-BATCH-001
system_design:
  - ../../specs/tasks/system-design/peer-report-batch-delivery.md
legacy_specs: []
---

# Implementation Plan: Peer report batch delivery

## Overview

A controller session supervising a wide fan-out receives one peer report per
child completion. Each report consumed one queue slot and one turn, so a burst
reached the per-session cap while the controller was still busy and the operator
saw `queue_full` for reports the controller would have consumed shortly after.

The selected design delivers a bounded, consecutive FIFO run of ordinary peer
reports as a single prompt, keeping per-report attribution, and settles every
reserved row exactly as the single-row path settles its one row.

## Requirement conformance and assumptions

The task system owns this capability because it owns the controller prompt queue
and its drain path. Queue admission and queued post-dispatch recovery remain
authoritative for their own concerns and are unchanged.

`queue_full` remains reachable while a controller turn is long-running and only
one batch has been dispatched. This limitation is stated in the requirement and
the design and is not claimed away.

No user-facing copy changes, so no i18n work is in scope.

## Work packages

- [Task 01: Batch reservation and eligibility](task-01-batch-peer-reports.md)
- [Task 02: Batch dispatch from the drain path](task-02-deliver-peer-report-batch.md)

## Results

Both work orders are implemented. The batch reserve, the shared eligibility rule,
the composition with per-report attribution, the all-rows requeue, and both drain
integrations landed together, because the eligibility rule has to be shared by
reservation and by the drain helper.

## Verification

Run from `apps/backend` in the worktree, restricted to changed packages.

```bash
go test -p 2 -tags fts5 ./internal/orchestrator/messagequeue/... -count=1
go test -p 2 -tags fts5 ./internal/orchestrator/ -run "Drain|Queue|PeerReportBatch" -count=1
golangci-lint run ./... --new-from-rev="origin/main" --timeout=5m
```

`TestHandleAgentReady_PassthroughQueuedAttachmentUsesAttachmentAwarePrompt` fails
on the base branch as well and is unrelated to this change.