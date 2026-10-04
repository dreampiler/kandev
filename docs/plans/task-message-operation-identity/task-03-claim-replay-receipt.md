---
id: "03-claim-replay-receipt"
title: "Claim before dispatch, replay as a read, return a receipt"
status: draft
wave: 2
depends_on: ["02-operation-record"]
plan: "plan.md"
requirements:
  - REQ-PLATFORM-TASK-MSG-OPID-001
  - REQ-PLATFORM-TASK-MSG-OPID-003
system_design:
  - ../../specs/platform/system-design/task-message-operation-identity.md
acceptance_criteria:
  - AC-PLATFORM-TASK-MSG-OPID-001.1
  - AC-PLATFORM-TASK-MSG-OPID-001.2
  - AC-PLATFORM-TASK-MSG-OPID-001.3
  - AC-PLATFORM-TASK-MSG-OPID-003.1
  - AC-PLATFORM-TASK-MSG-OPID-003.2
  - AC-PLATFORM-TASK-MSG-OPID-003.3
  - AC-PLATFORM-TASK-MSG-OPID-003.4
---

# Task 03: Claim before dispatch, replay as a read, return a receipt

## Summary

Thread the optional `operation_id` through the message-task handler, claim it
before the first delivery side effect, settle it after, replay a known identity
as a pure read, and return the delivered artifact's identity in the response.

## In scope

The new extracted handler file that owns claim, replay, the direct
request-field comparison, settlement, and receipt assembly; the single seam
call and the new request parameter in `handleMessageTask`; and the tool
declaration with its parameter and description. Settlement runs on a context
detached from the request so a client disconnect cannot drop the settlement of
a delivery that committed.

## Out of scope

The readback tool and retention, owned by Task 04. Any change to dispatch
ordering, admission, or delivery-mode semantics.

## Acceptance

- With no `operation_id`, the send behaves exactly as today and writes no
  operation row.
- With an `operation_id`, the row exists before the dispatch seam runs, and the
  success response echoes `operation_id` plus `message_id` and/or
  `queued_entry_id` when applicable, leaving the existing `task_id`,
  `session_id`, and `status` fields unchanged.
- A replay whose stored prompt, delivery mode, and requested session all equal
  the replayed values returns the recorded status and creates no second
  message, no second queue entry, and no turn, and issues no interrupt.
- A replay differing in any stored request field is rejected with the stable
  `operation_id_conflict` code and delivers nothing.
- A send carrying both `reply_to_question_id` and `operation_id` is claimed
  once and delivered once.

## Verification

```bash
go test -race ./internal/mcp/handlers -count=1 -run 'MessageTask|Operation'
golangci-lint run ./... --new-from-rev="<base-sha>" --timeout=5m
```

## Files likely touched

- `apps/backend/internal/mcp/handlers/message_task_operation.go`
- `apps/backend/internal/mcp/handlers/message_task_operation_test.go`
- `apps/backend/internal/mcp/handlers/handlers.go`
- `apps/backend/internal/mcp/server/server.go`

## Dependencies

- 02-operation-record

## Risks

`handleMessageTask` lives in a file already at the revive file-size limit, so
all new logic must land in new files. Compare request fields by value; do not
introduce a digest, hash, or signature.

## Parallelism

Runs after Task 02. Disjoint from Task 04 except for the shared tool
registration site, so those two edits are sequenced.

## Inputs

- [Requirements](../../specs/platform/requirements/task-message-operation-identity.md)
- [System design](../../specs/platform/system-design/task-message-operation-identity.md)
- `apps/backend/internal/mcp/handlers/handlers.go`, `handleMessageTask` and its
  dispatch helpers
- `apps/backend/AGENTS.md`, code-quality limits

## Results

Not started.