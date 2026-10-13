---
status: draft
system: tasks
created: 2026-10-13
owners:
  - kandev
---

# MCP: Control Task Targeting Requirements

## Overview

A control (monitoring) agent often knows a task only by a short id (the first
eight characters shown in a board or a report) and must act on tasks that belong
to no workflow, such as an automation-run output. The MCP task tools resolved
neither: `archive_task_kandev` with a short id failed, and `list_tasks_kandev`
required a `workflow_id`, so a workflow-less task could not be found.

This requirement adds short-id resolution and a workspace-scoped task list to
the MCP task surface. It does not widen any tool's authorization: the resolved
id is still subject to every existing guard.

## Terminology

- **Short task id:** A hex prefix of a UUID, 8 to 35 characters.

## Requirements

### REQ-TASKS-MCP-TARGET-001: A task-id argument accepts a short prefix

**Intent:** A control agent that knows only the first eight characters of a task
id should be able to act on it, without a round trip to fetch the full id first.

#### Acceptance criteria

- **AC-TASKS-MCP-TARGET-001.1:** A task-id argument (`task_id`,
  `caller_task_id`, `source_task_id`, `parent_id`, `depends_on_task_id`) given as
  a short task id shall be resolved to the full id when exactly one task in the
  caller's workspace starts with it.
- **AC-TASKS-MCP-TARGET-001.2:** When more than one task in the caller's
  workspace starts with the prefix, the request shall be rejected with
  `validation` and a candidate list of `{task_id, title, state}`; no task shall be
  mutated.
- **AC-TASKS-MCP-TARGET-001.3:** A full UUID, a non-hex value, or a prefix that
  matches no task in the caller's workspace shall be passed through unchanged, so
  the target tool keeps its own not-found behavior.
- **AC-TASKS-MCP-TARGET-001.4:** Resolution shall be scoped to the caller's own
  workspace and shall never resolve a task in another workspace.
- **AC-TASKS-MCP-TARGET-001.5:** Resolution shall not widen any tool's
  authorization; the resolved id is still checked by every existing guard.

### REQ-TASKS-MCP-TARGET-002: list_tasks_kandev lists a workspace

**Intent:** A control agent should find a workspace's tasks, including the ones
that belong to no board, without a `workflow_id` it cannot get.

#### Acceptance criteria

- **AC-TASKS-MCP-TARGET-002.1:** `list_tasks_kandev` shall accept an optional
  `workspace_id`. With it, the tool returns the workspace's non-archived,
  non-ephemeral tasks, including tasks whose `workflow_id` is empty.
- **AC-TASKS-MCP-TARGET-002.2:** A non-Office caller shall only name its own
  workspace; another workspace shall be refused with `forbidden`. Cross-workspace
  listing stays on the Office reach path (`REQ-OFFICE-XWS-003`).
- **AC-TASKS-MCP-TARGET-002.3:** With only `workflow_id`, or with neither field,
  the existing behavior shall be unchanged.

## Out of scope

- Exposing automation-origin tasks through `list_tasks_kandev`. They stay hidden
  by provenance for a non-Office caller; widening that is a separate decision
  about which principal counts as a controller.
- Session-id prefix resolution. Only task-id arguments are resolved here.
