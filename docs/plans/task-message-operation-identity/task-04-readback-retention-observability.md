---
id: "04-readback-retention-observability"
title: "Expose the readback, bound retention, observe it"
status: draft
wave: 3
depends_on: ["02-operation-record", "03-claim-replay-receipt"]
plan: "plan.md"
requirements:
  - REQ-PLATFORM-TASK-MSG-OPID-002
  - REQ-PLATFORM-TASK-MSG-OPID-004
system_design:
  - ../../specs/platform/system-design/task-message-operation-identity.md
acceptance_criteria:
  - AC-PLATFORM-TASK-MSG-OPID-002.1
  - AC-PLATFORM-TASK-MSG-OPID-002.2
  - AC-PLATFORM-TASK-MSG-OPID-002.3
  - AC-PLATFORM-TASK-MSG-OPID-004.1
  - AC-PLATFORM-TASK-MSG-OPID-004.2
  - AC-PLATFORM-TASK-MSG-OPID-004.3
  - AC-PLATFORM-TASK-MSG-OPID-004.4
---

# Task 04: Expose the readback, bound retention, observe it

## Summary

Give the caller a side-effect-free way to resolve an operation identity to its
recorded outcome, keep the record store bounded, and make claim, replay, and
conflict visible without leaking identities into metrics.

## In scope

The new `get_task_message_operation_kandev` tool and its handler, scoped to the
calling session's own sender pair and returning `state`, `delivery_status`,
`message_id`, `queued_entry_id`, `failure_code`, timestamps, and `retry_safe`;
bounded batched retention of settled and stale-pending rows under existing
maintenance admission; the closed-set metric; and the observability
documentation bullet.

## Out of scope

Any change to `get_task_conversation_kandev`. Any frontend surface. Retrying or
cancelling on the caller's behalf.

## Acceptance

- A readback performs no delivery and reports `pending` as in flight with an
  explicit do-not-retry signal, `committed` as done, `failed` as the only
  state where `retry_safe` is true, and an unknown identity as a conclusive
  not-claimed result.
- A readback naming another session's operation is denied without disclosing
  whether it exists, and a rejected replay echoes no stored content.
- Retention removes rows in bounded batches under existing maintenance
  admission and never blocks or fails a live send; a stale-pending age sweep
  settles very old `pending` rows as `failed` with `outcome_unknown`.
- `task_message_send_operation_total` carries only `outcome` from
  `claimed`, `replayed`, `conflict_rejected` and `delivery_status` from
  `sent`, `queued`, `started`.

## Verification

```bash
go test -race ./internal/mcp/handlers ./internal/system/maintenance -count=1
```

## Files likely touched

- `apps/backend/internal/mcp/handlers/get_task_message_operation.go`
- `apps/backend/internal/mcp/handlers/get_task_message_operation_test.go`
- `apps/backend/internal/mcp/server/server.go`
- `apps/backend/internal/system/maintenance/`
- `apps/backend/AGENTS.md`
- `AGENTS.md`

## Dependencies

- 02-operation-record
- 03-claim-replay-receipt

## Risks

The readback is a new session-keyed entry point and must authorize through the
existing `authorize*` helpers before touching any dependency. Retention shares
admission with backup and restore, so its batch bounds must be respected.

## Parallelism

Limited to the shared tool-registration site, which is sequenced with Task 03.

## Inputs

- [Requirements](../../specs/platform/requirements/task-message-operation-identity.md)
- [System design](../../specs/platform/system-design/task-message-operation-identity.md)
- `apps/backend/internal/mcp/handlers/get_task_conversation.go`
- `apps/backend/AGENTS.md`, scoping and observability sections

## Results

Not started.