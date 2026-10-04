---
id: "05-integration-verification"
title: "Verify the change and synchronize documentation"
status: draft
wave: 4
depends_on: ["03-claim-replay-receipt", "04-readback-retention-observability"]
plan: "plan.md"
requirements:
  - REQ-PLATFORM-TASK-MSG-OPID-001
  - REQ-PLATFORM-TASK-MSG-OPID-002
  - REQ-PLATFORM-TASK-MSG-OPID-003
  - REQ-PLATFORM-TASK-MSG-OPID-004
system_design:
  - ../../specs/platform/system-design/task-message-operation-identity.md
acceptance_criteria:
  - AC-PLATFORM-TASK-MSG-OPID-001.2
  - AC-PLATFORM-TASK-MSG-OPID-002.4
---

# Task 05: Verify the change and synchronize documentation

## Summary

Run the changed scope for real, prove that identity-free sends are unregressed,
and bring the documentation in line with what landed.

## In scope

The full backend test and lint run, the schema guard, the specification
validation pass, and any documentation synchronization the implementation
actually required.

## Out of scope

Further behavior change. New tests beyond what Tasks 02 through 04 own, except
where a gap is found in verification.

## Acceptance

- The existing message-task suites pass unmodified. That unmodified pass is the
  regression proof for AC-001.2, because every one of them sends without an
  `operation_id`.
- Every acceptance criterion in the requirement document is covered by a test
  that names it, or by the unmodified existing suites listed below.
- If the implementation introduced a new durable rule, such as a new entry
  point that must authorize, the backend `AGENTS.md` and the root `AGENTS.md`
  observability list reflect it. If it did not, neither file changes.

## Verification

```bash
make -C apps/backend test
make -C apps/backend lint
go run ./cmd/sqlguard ./internal
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
```

## Files likely touched

None unless a documentation gap is found.

## Dependencies

- 03-claim-replay-receipt
- 04-readback-retention-observability

## Risks

The full backend suite is long. Run it to completion in the foreground and read
the exit code rather than inferring status from a partial log.

## Parallelism

Runs last; nothing may edit the same files concurrently.

## Inputs

- [Requirements](../../specs/platform/requirements/task-message-operation-identity.md)
- [System design](../../specs/platform/system-design/task-message-operation-identity.md)
- The existing message-task suites in `apps/backend/internal/mcp/handlers`:
  `message_task_test.go`, `message_task_readiness_test.go`,
  `message_task_initial_launch_test.go`,
  `message_task_initial_launch_rollback_test.go`,
  `message_task_queue_transfer_test.go`,
  `message_task_turn_ownership_test.go`

## Results

Not started.