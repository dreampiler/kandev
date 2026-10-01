---
id: "04-prompt-rollback-ownership"
title: "Preserve prompt rollback ownership"
status: done
wave: 3
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001
acceptance_criteria:
  - AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.5
  - AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.6
  - AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.7
system_design:
  - ../../specs/tasks/system-design/queued-post-dispatch-recovery.md
---

# Task 04: Preserve Prompt Rollback Ownership

## Scope

Implement the existing [plan](plan.md) ownership repair and
[prompt rollback design](../../specs/tasks/system-design/queued-post-dispatch-recovery.md#prompt-rollback-ownership).
Rejected MCP compensation cleans only its own unreferenced unsent turn.
Missing-own-turn compensation restores only an unaccepted still-owned claim
through existing guards and snapshot CAS. Accepted work retains its transcript
and turn. No global CAS, schema, runtime recovery policy, or watchdog changes.

## Files

- `apps/backend/internal/mcp/handlers/handlers.go`
- `apps/backend/internal/orchestrator/service.go`
- `apps/backend/internal/orchestrator/task_operations.go`
- `apps/backend/internal/task/repository/sqlite/session.go`
- `apps/backend/internal/task/service/service_turns.go`
- `apps/backend/internal/task/repository/sqlite/session_test.go`
- `apps/backend/internal/mcp/handlers/message_task_turn_ownership_test.go`
- `apps/backend/internal/orchestrator/prompt_rollback_missing_turn_test.go`

## Verification and results

Actual-function temporary-SQLite RED tests reproduced two ownership failures;
callback successor and accepted-response RED tests reproduced three further
assertion failures. The implemented guards passed focused regression tests.
The SQLite snapshot fixture explicitly supplies a distinct competing revision,
since adjacent Windows clock reads may share a timestamp; production CAS is
unchanged. Arbitrary same-timestamp metadata changes remain an existing limit.

From `apps/backend`, the focused ownership and adjacent queue regressions passed
with `go test -race -p 2 -tags fts5 -count=1 -run` in `./internal/mcp/handlers`,
`./internal/orchestrator`, `./internal/task/repository/sqlite`, and
`./internal/task/service`. Cases cover rejection/adoption, missing own turn,
successor revision/execution, CAS zero rows, accepted transcript, FIFO and
Auto-run, long provider protection, and TurnRemoved database readback.
Existing changed-package lint reported zero issues. No listeners, full suite,
E2E, operating database mutation, or installation replacement was performed.
