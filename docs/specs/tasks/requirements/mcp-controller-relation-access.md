---
status: draft
system: tasks
created: 2026-10-13
owners:
  - kandev
---

# MCP: Controller Relation-Transcending Access Requirements

## Overview

A control (monitoring) agent that manages a workspace's tasks must discover and
inspect tasks it did not create and is not related to. `list_related_tasks_kandev`
denied such targets (`FORBIDDEN` for anything outside self / ancestor /
descendant / sibling / blocker), so a controller could not read a task's parent,
children, or blockers just because no direct relation edge existed. This
requirement opens that single read gate to a controller principal, within the
principal's own workspace, and to nothing else.

## Terminology

- **Controller principal:** A principal whose own task is a workspace root task
  (empty `parent_id`) with at least one child.

## Requirements

### REQ-TASKS-MCP-CTRL-001: A controller may list an in-workspace task's relations

**Intent:** A controller should manage a workspace's tasks from a visible place
without routing every unrelated task through the operational session.

#### Acceptance criteria

- **AC-TASKS-MCP-CTRL-001.1:** `list_related_tasks_kandev` shall exempt a
  controller principal from the direct-relation gate when the target task is in
  the principal's own workspace, returning the target's parent, children,
  siblings, blockers, and blocked-by tasks.
- **AC-TASKS-MCP-CTRL-001.2:** A principal that is not a controller shall be
  refused exactly as today (`FORBIDDEN`) for an unrelated target.
- **AC-TASKS-MCP-CTRL-001.3:** The exemption shall remain workspace-bounded; a
  target in another workspace shall still be refused (`FORBIDDEN`), leaving
  cross-workspace access on the Office reach path.
- **AC-TASKS-MCP-CTRL-001.4:** The controller shall be derived from the
  server-side principal; an agent shall not widen its scope by naming a
  controller task as `caller_task_id`.
- **AC-TASKS-MCP-CTRL-001.5:** No new permission model shall be introduced; the
  discriminator and the gate reuse existing task-service seams.

## Out of scope

- Cross-workspace control writes (stop, move, re-prioritize, archive, delete);
  they stay forbidden for another workspace (`REQ-OFFICE-XWS-004`).
- Plan reads/writes and step moves: those handlers carry no relation gate in the
  same workspace, so no widening is required here.
- Exposing automation-origin tasks through `list_tasks_kandev`.
