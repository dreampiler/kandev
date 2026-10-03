---
id: TASK-COORDINATOR-CHILD-TASK-ORDERING-001
title: Tool and Replay Order
status: done
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001
  - REQ-AGENTS-SESSION-CEILING-001
acceptance_criteria:
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.1
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.2
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.3
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.4
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.5
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.6
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.7
  - AC-AGENTS-SESSION-CEILING-001.11
system_design:
  - ../../specs/tasks/system-design/coordinator-child-task-ordering.md
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Tool and Replay Order

Plan: [Coordinator Child Task Ordering](plan.md)

## Requirements and design

- `REQ-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001`, acceptance criteria .1 through .7:
  [requirements](../../specs/tasks/requirements/coordinator-child-task-ordering.md),
  [design](../../specs/tasks/system-design/coordinator-child-task-ordering.md).
- `REQ-AGENTS-SESSION-CEILING-001`, `AC-AGENTS-SESSION-CEILING-001.11`:
  [requirements](../../specs/agents/requirements/session-concurrency-ceiling.md),
  [design](../../specs/agents/system-design/session-concurrency-ceiling.md).

## Implementation scope

- Own MCP handler, registration, forwarding, metadata, injected guidance, and
  child-order tests under `internal/mcp/` and `internal/sysprompt/`.
- Add the WebSocket action in `pkg/websocket/actions.go`.
- Export the existing priority rank from `internal/task/models/step_order.go`.
- Own `internal/orchestrator/ceiling_replay_order.go` and its focused tests;
  add sorting before replay without changing retry pacing or payload handling.
- Update the paired specs and public coordination and MCP reference.

## Acceptance and validation

Prove both placements, unauthorized and mixed targets, conflict retry, trusted
sender forwarding, mode availability, priority/position/time/ID comparison,
and stability under unrelated writes and repeated refusal bookkeeping.

Run sequentially from `apps/backend`:

```text
go test -p 2 -count=1 ./internal/mcp/handlers/ ./internal/mcp/server/ ./pkg/websocket/
go test -p 2 -count=1 ./internal/orchestrator/ -run Ceiling
go test -p 2 -count=1 ./internal/task/models/ ./internal/task/service/ ./internal/task/repository/sqlite/ -run "Reorder|StepOrder|CeilingDeferred"
golangci-lint run --new-from-rev=origin/main ./internal/mcp/... ./internal/orchestrator/... ./internal/task/... ./pkg/websocket/...
```

Check changed Go formatting, spec catalog and lint, and both public-doc validators.
No frontend source changes or runtime deployment are included. Existing Windows
listener tests that open firewall prompts are outside this package selection.

## Risks

Band membership can change during a call; retry once, then surface the conflict.
Global ceiling replay is priority-first while per-step admission remains
position-first. Existing sessions need refreshed MCP registration after deployment.

## Results

All targeted checks passed on the implementation branch:

- `go test -p 2 -count=1 -timeout 5m ./internal/mcp/handlers/ ./internal/mcp/server/ ./pkg/websocket/`:
  exit 0, including dispatcher-to-SQLite readback, publication, and next queued
  candidate selection. Handlers 71.310s, server 6.497s, WebSocket 0.487s.
- `go test -p 2 -count=1 -timeout 5m ./internal/orchestrator/ -run Ceiling`:
  exit 0, 21.646s, including the existing retry schedule and payload tests.
- `go test -p 2 -count=1 -timeout 5m ./internal/task/models/ ./internal/task/service/ ./internal/task/repository/sqlite/ -run "Reorder|StepOrder|CeilingDeferred"`:
  exit 0, models 0.593s, service 1.611s, SQLite 4.833s.
- `golangci-lint run --timeout=10m --new-from-rev=origin/main ./internal/mcp/... ./internal/orchestrator/... ./internal/task/... ./pkg/websocket/...`:
  exit 0, zero issues.
- `gofmt -l` on all changed Go files: exit 0, no output.
- `python scripts/list-docs.py validate` and
  `python scripts/lint-spec-files.py --all`: exit 0.
- `node --test scripts/validate-public-docs.test.mjs`: exit 0, 62 passed.
- `node scripts/validate-public-docs.mjs`: exit 0, 47 pages validated.
- Existing PR documentation-coverage validator with the work order, plan, and
  referenced requirements/designs: exit 0, accepted both requirement mappings.

The initial tests exposed stale parent authorization in the band snapshot and
the ID-only sort's missing priority/time behavior; both were corrected before
the green package runs. The existing reorder service detects membership
conflicts, with no revision precondition for same-membership concurrent
reorders. No runtime deployment, frontend code, or normal admission ordering
change is included.
