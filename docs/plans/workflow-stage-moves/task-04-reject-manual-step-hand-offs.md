---
id: "04-reject-manual-step-hand-offs"
title: "Reject move_task hand-offs to manual steps and surface move reasons"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WORKFLOW-STAGE-MOVES-004
acceptance_criteria:
  - AC-TASKS-WORKFLOW-STAGE-MOVES-004.1
  - AC-TASKS-WORKFLOW-STAGE-MOVES-004.2
  - AC-TASKS-WORKFLOW-STAGE-MOVES-004.3
  - AC-TASKS-WORKFLOW-STAGE-MOVES-004.4
  - AC-TASKS-WORKFLOW-STAGE-MOVES-004.5
system_design:
  - ../../specs/tasks/system-design/workflow-stage-moves.md
---

# Work Order 04: Reject move_task hand-offs to manual steps and surface move reasons

## Outcome

`move_task_kandev` rejects a `prompt` or `entry_options` for a step that does
not start work on entry, before the deferred or immediate split. Known move
failures tell the agent the reason without internal identifiers.

## In scope

- `apps/backend/internal/workflow/models`: add `StepRunsOnEntry` only. Do not
  add other step-semantics helpers.
- `apps/backend/internal/workflow/move/errors.go` and `normalization.go`:
  `ErrEntryTargetIsManual` and `ValidateEntryTarget`.
- `apps/backend/internal/mcp/handlers/config_task_handlers.go`: the
  manual-target check in `handleMoveTask`, classification of the new sentinel,
  and reason text in `moveTaskErrorMessage`. Unrecognized errors keep the
  existing generic text.
- `apps/backend/internal/mcp/server/server.go`, `config_handlers.go`, and
  `handlers.go`: `move_task_kandev`, `prompt`, and `entry_options`
  descriptions.
- `docs/public/agent-communication.md`, `docs/public/websocket-api.md`, and
  `docs/specs/workflow-step-move-overrides/spec.md`: state the agent-surface
  rule.

## Out of scope

- HTTP and WebSocket `task.move` validation.
- Entry-option semantics other than the manual-step rule.

## Acceptance conditions

1. A step change to a manual step with entry options returns `VALIDATION` with
   the manual-step reason, and records no pending move. A step that starts
   work on entry accepts the options.
2. Known conflict and validation reasons appear in the agent-facing text. A
   WIP-limit error does not expose the step identifier. An unrecognized error
   keeps the generic text.
3. Both tool registrations describe the rule.

## Verification

Extend the focused tests in `mcp/handlers/config_handlers_test.go`, and add
unit tests for `StepRunsOnEntry` and `ValidateEntryTarget`. Run them before the
fix (expected to fail) and after it:

```bash
cd apps/backend
go test ./internal/mcp/handlers/ -run 'MoveTask' -count=1
go test ./internal/workflow/move/ ./internal/workflow/models/ -run 'EntryTarget|RunsOnEntry' -count=1
go test ./internal/mcp/server/ -run 'MoveTask|ToolDescription' -count=1
```

## Likely files

See In scope. Dependencies: none.

## Results

Pending.
