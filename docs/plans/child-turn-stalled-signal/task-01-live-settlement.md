---
id: "01-live-settlement"
title: "Capture live settlement and durable identity"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-CHILD-STALL-001
  - REQ-TASKS-CHILD-STALL-002
acceptance_criteria:
  - AC-TASKS-CHILD-STALL-001.1
  - AC-TASKS-CHILD-STALL-001.2
  - AC-TASKS-CHILD-STALL-002.1
  - AC-TASKS-CHILD-STALL-002.3
system_design:
  - ../../specs/tasks/system-design/child-turn-stalled-signal.md
---

# Task 01: Capture Live Settlement and Durable Identity

## Summary and scope

Capture the task's workflow entry and parent/workspace at turn start, distinguish
accepted live execution from reserve/abandon/cancel paths, and persist one
candidate alongside genuine live completion. Add the narrow internal event as
an acceleration hint, not durable storage. Read backend scoped guidance first.

Likely files: `apps/backend/internal/task/service/service_turns.go`,
`internal/task/models/` turn models, `internal/task/repository/interface.go`,
`internal/task/repository/sqlite/conversation_receipts.go`, repository schema
and cleanup registry, and `internal/events/` event constants. Paths beginning
with `internal/` are relative to `apps/backend`.

## Exclusions

No classification, parent enqueue, workflow transition, frontend change,
historical backfill of guessed live provenance, or permanent tests.

## Implementation acceptance

1. Start snapshot is consistent with entry identity, including reserved-turn
   dispatch acceptance; unknown provenance never becomes a deliverable candidate.
2. Live completion and unique candidate persist together; orphan closure and
   cancelled/rejected dispatch cannot masquerade as live completion.
3. Duplicate completion and a crash before bus publication retain one recoverable
   identity on both supported database dialects without changing legacy events.

## Verification

From `apps/backend`, run the existing narrow compatibility checks:

```text
go test -p 2 -tags fts5 ./internal/task/service -run 'Test(CompleteTurn|AbandonOpenTurns)'
```

For new scenarios, author temporary overlay cases named `TestChildTurnSettlement`
under `$env:TEMP\kandev-child-turn-stalled-signal`, with virtual test files only
in the intended packages. Explicitly invoke the overlay; ordinary tests must
not collect these files:

```text
go test -p 2 -tags fts5 -overlay "$env:TEMP\kandev-child-turn-stalled-signal\overlay.json" ./internal/task/service ./internal/task/repository/sqlite -run '^TestChildTurnSettlement'
```

Cover start/transition races, dispatch reserve/accept/reject, unknown identity,
live/abandoned distinction, duplicate completion, and completion/outbox rollback.
Record SQLite results and the existing environment-gated PostgreSQL result or
specific unavailable test environment. Remove owned temporary files afterward.

## Dependencies, risks, results

Requires owner selection of A and explicit implementation instruction.
Completion callers may not all represent live settlement; trace them before
wiring the new producer. Results: not implemented; no tests run in design turn.
