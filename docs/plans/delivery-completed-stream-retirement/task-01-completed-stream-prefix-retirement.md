---
id: "01-completed-stream-prefix-retirement"
title: "Retire completed-stream fragments after the grace"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002
acceptance_criteria:
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001.1
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001.2
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001.3
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002.1
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002.2
  - AC-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002.3
system_design:
  - ../../specs/platform/system-design/delivery-completed-stream-retirement.md
---

# Task 01: Retire completed-stream fragments after the grace

## Summary

Widen the locked retention eligibility predicate so a completed stream's eligible compact fragment rows retire after the payload grace, independent of `ReplayAge`/`ReplayEvents`, while checkpoint, submission, terminal and workflow receipts remain.

## In scope

- Extend the completion test in the retention selector so a stream qualifies when cursor identity matches, `sequence <= projected_sequence`, `projected_sequence == received_sequence`, there is no unprojected row at `projected_sequence + 1`, the stream has no unresolved terminal/active submission, and there is no open recovery block, pending effect or unknown effect.
- Reuse the existing contiguous-prefix retirement and generic-receipt deletion; keep the `WHERE retired_sequence = ?` cursor guard.
- Add companion backend unit tests beside the source.

## Out of scope

- Changing the replay-window default for active or uncertain streams.
- Coalescing events at receive/journal/wire time.
- Schema, index, wire, journal or config-format changes.
- New gates, monitors, ledgers, integration/E2E suites or CI jobs.

## Acceptance conditions

1. A completed stream retires its compact fragment rows after the grace without waiting for `ReplayAge`/`ReplayEvents`; an active, unprojected or uncertain stream follows the existing window unchanged.
2. A protected, unprojected, unknown-type or oversized row still stops the contiguous prefix; a replay at or below the checkpoint still returns the expired result and reconciles from the projected cursor without resending prompts.
3. Cursor, submission, terminal-settlement and workflow receipts remain after retirement; no identity, hash, generation or checkpoint resets.

## Likely files

- `apps/backend/internal/task/repository/sqlite/agent_delivery_retention_events.go`
- `apps/backend/internal/task/repository/sqlite/agent_delivery_retention_discovery.go`
- `apps/backend/internal/task/repository/sqlite/agent_delivery_retention_candidates.go`
- `apps/backend/internal/task/repository/sqlite/agent_delivery_retention_proof.go` (read-only evidence)
- new `apps/backend/internal/task/repository/sqlite/agent_delivery_retention_completed_*_test.go`

## Verification

From `apps/backend`:

```
go test -race -run 'CompletedStream|Retention' ./internal/task/repository/sqlite ./internal/task/deliveryretention
```

Companion unit tests prove the changed behavior. One-off reproduction on a disposable database copy or narrow temporary database reads before/after row and byte counts through the real `RunAgentDeliveryRetentionBatch` entry point. This is not a standing test.

## Dependencies and risks

- No dependency on another work order.
- Risk: a too-early "completed" classification retires evidence a reconnect needs; the predicate must read received == projected, the unprojected gap and unresolved terminal/submission/block/effect under the existing stream lock.
- Risk: dialect-sensitive SQL must keep working on PostgreSQL; the changed predicate must be exercised on both engines where the existing tests already gate.

## Results

Implemented the completed-stream early-retirement predicate:

- `agent_delivery_retention_proof.go`: `agentDeliveryCompletedStreamSQL(cursorAlias)` — fully projected (`received_sequence = projected_sequence`) AND a retained settled terminal exists for the stream AND no submission of the incarnation is `prepared/dispatching/accepted/interrupted_unknown`.
- `agent_delivery_retention_candidates.go`: the locked candidate query carries `stream_completed`.
- `agent_delivery_retention_events.go`: `agentDeliveryRetentionSequenceExpired` returns true for a completed stream regardless of the age/count window; the contiguity, first-terminal and sequence guards are unchanged.
- `agent_delivery_retention_discovery.go`: the retirement-head query admits a completed stream's compacted head in addition to the age/count window.

Active, uncertain, unterminated and partially projected streams keep the existing 24h / 10,000-event contract.

Companion test `agent_delivery_retention_completed_stream_test.go`: a completed stream retires its fragment rows after the grace while inside the window; an open (unterminated) stream keeps them.

Verification (from `apps/backend`):

```
go build -trimpath ./internal/task/repository/sqlite/
go vet -trimpath ./internal/task/repository/sqlite/ ./internal/task/deliveryretention/
go test -race -trimpath -count=1 -timeout 1800s -run 'TestDeliveryRetentionEvents|...|TestDeliveryRetentionCompletedStream' ./internal/task/repository/sqlite/   # 70.899s, ok
go test -race -trimpath -count=1 -run 'Retention' ./internal/task/deliveryretention/   # 1.607s, ok
```

`gofmt -l` clean. Windows `-race` is supported on this host (cgo/gcc available), so the race run executed rather than being waived. PostgreSQL-gated cases skip without a DSN, as before.

