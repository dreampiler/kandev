---
id: "02-operation-record"
title: "Add the durable operation record"
status: draft
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-TASK-MSG-OPID-001
  - REQ-PLATFORM-TASK-MSG-OPID-002
system_design:
  - ../../specs/platform/system-design/task-message-operation-identity.md
acceptance_criteria:
  - AC-PLATFORM-TASK-MSG-OPID-001.3
  - AC-PLATFORM-TASK-MSG-OPID-002.4
---

# Task 02: Add the durable operation record

## Summary

Add the `task_message_send_operations` table, its model, and the repository that
claims, reads, and settles operation rows, so an operation identity survives a
backend restart.

## In scope

The new table and its unique constraint on
`(sender_task_id, sender_session_id, operation_id)`, the
`TaskMessageSendOperation` model with `MetaKeySendOperationID`, the repository
interface plus its SQLite implementation that also serves PostgreSQL, the
idempotent migration, and registration in `requiredstores` and
`storeconformance`. Store the delivery-determining request fields verbatim;
add no derived digest column.

## Out of scope

Any handler, MCP tool, or wire change. Task 03 owns those.

## Acceptance

- A claim is a single-row insert that commits independently of dispatch, and a
  second claim for the same identity loses on the unique constraint, classified
  by constraint name rather than message text.
- Settlement is a single conditional update guarded by `state = 'pending'`, so
  a late or duplicate settle cannot rewrite a terminal outcome.
- Operation rows are ordinary committed rows, so a read works immediately after
  a backend restart with no recovery step.

## Verification

```bash
go run ./cmd/sqlguard ./internal
go test -race ./internal/persistence/storeconformance -count=1
go test ./internal/task/repository/sqlite -count=1
KANDEV_TEST_POSTGRES_DSN=<dsn> go test ./internal/task/repository/sqlite -count=1 -run Postgres
```

## Files likely touched

- `apps/backend/internal/task/models/models.go`
- `apps/backend/internal/task/models/message_send_operation.go`
- `apps/backend/internal/task/repository/interface.go`
- `apps/backend/internal/task/repository/sqlite/task_message_send_operation.go`
- `apps/backend/internal/task/repository/sqlite/base_migrations.go`
- `apps/backend/internal/persistence/requiredstores/`
- `apps/backend/internal/persistence/storeconformance/`

## Dependencies

None.

## Risks

The schema-registration ceremony is easy to under-do and boot fails readiness
without it. A brand-new table needs no `ADD COLUMN` step, but fresh-DB and
same-DB replay tests are still required.

## Parallelism

Disjoint from Tasks 03 and 04 once the repository interface in this work order
is frozen.

## Inputs

- [Requirements](../../specs/platform/requirements/task-message-operation-identity.md)
- [System design](../../specs/platform/system-design/task-message-operation-identity.md)
- The `external_id` identity implementation in
  `apps/backend/internal/task/repository/sqlite/task_external_id.go`
- `apps/backend/AGENTS.md`, schema and migration sections

## Results

Not started.