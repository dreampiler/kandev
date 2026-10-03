---
id: PLAN-COORDINATOR-CHILD-TASK-ORDERING
title: Coordinator Child Task Ordering
status: done
requirements:
  - REQ-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001
  - REQ-AGENTS-SESSION-CEILING-001
system_design:
  - ../../specs/tasks/system-design/coordinator-child-task-ordering.md
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Coordinator Child Task Ordering Plan

## Scope

Implement the task-mode child ordering tool and priority-first ceiling replay.
Preserve normal position-first WIP admission and the existing ceiling retry
schedule. Runtime deployment and workspace materialization recovery are excluded.

## Sources

- [Child ordering requirements](../../specs/tasks/requirements/coordinator-child-task-ordering.md)
- [Child ordering design](../../specs/tasks/system-design/coordinator-child-task-ordering.md)
- [Session ceiling requirements](../../specs/agents/requirements/session-concurrency-ceiling.md)
- [Session ceiling design](../../specs/agents/system-design/session-concurrency-ceiling.md)

## Work packages

| Work order | Status | Dependencies |
| --- | --- | --- |
| [Tool and replay order](task-01-tool-and-replay-order.md) | done | Existing step reorder service and ceiling retry schedule |
