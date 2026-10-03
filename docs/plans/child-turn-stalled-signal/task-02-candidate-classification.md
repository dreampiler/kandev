---
id: "02-candidate-classification"
title: "Classify durable candidates"
status: pending
wave: 2
depends_on:
  - "01-live-settlement"
plan: "plan.md"
requirements:
  - REQ-TASKS-CHILD-STALL-001
  - REQ-TASKS-CHILD-STALL-002
  - REQ-TASKS-CHILD-STALL-003
acceptance_criteria:
  - AC-TASKS-CHILD-STALL-001.1
  - AC-TASKS-CHILD-STALL-001.2
  - AC-TASKS-CHILD-STALL-001.3
  - AC-TASKS-CHILD-STALL-002.3
  - AC-TASKS-CHILD-STALL-002.4
  - AC-TASKS-CHILD-STALL-003.2
  - AC-TASKS-CHILD-STALL-003.3
system_design:
  - ../../specs/tasks/system-design/child-turn-stalled-signal.md
---

# Task 02: Classify Durable Candidates

## Summary and scope

Bind the existing monitor to the internal feed after identifying its actual
entrypoint and structured cause evidence. Add leased claims, current-state
qualification, suppression, capped retries, restart reconciliation, and resolved
retention. Likely files: new focused helpers under
`apps/backend/internal/task/service/`, task repository interface/SQLite files,
and `apps/backend/internal/backendapp/` wiring; the external monitor binding is
unresolved and must be documented before any implementation of this order.

If no reusable monitor contract exists, return that fact for an owner decision;
do not quietly build an autonomous agent service. B is conditional on this seam.

## Exclusions

No paid inference, raw transcript event payload, parent prompt, workflow move,
new general event platform, public webhook, or permanent tests.

## Implementation acceptance

1. All listed causes qualify only with structured current evidence and the
   complete settlement predicate; ordinary success and optional signals suppress.
2. Failed reads and lost bus notifications recover through durable rows; parallel
   consumers, expired leases, and restart cannot duplicate classification delivery.
3. Retry exhaustion remains visible and retryable under the original identity;
   resolved retention prevents stale replay from recreating a candidate.

## Verification

Temporary cases named `TestChildTurnClassification` and `TestChildTurnReceipt`
use the Task 01 temporary overlay convention. From `apps/backend`:

```text
go test -p 2 -tags fts5 -overlay "$env:TEMP\kandev-child-turn-stalled-signal\overlay.json" ./internal/task/service ./internal/task/repository/sqlite -run '^TestChildTurn(Classification|Receipt)'
```

Cover all causes, required/optional signal, unknown evidence, ordinary success,
active execution, reparenting/workspace mismatch, archive/terminal state,
same-step re-entry, pending/committed deferred transition, read failure, duplicate
and reordered event, expired claim, restart, capped retry, and retention replay.
Assert no workflow mutation and no parent-slot acquisition during classification.
Record exact executed case counts and cleanup owned temporary files.

## Dependencies, risks, results

Depends on Task 01 and B selection. The monitor seam and authoritative cause
mapping are open implementation prerequisites, not confirmed existing services.
Results: not implemented; no tests run in design turn.
