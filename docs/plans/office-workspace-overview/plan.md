---
created: 2026-09-29
status: complete
requirements:
  - REQ-OFFICE-WORKSPACE-OVERVIEW-001
  - REQ-OFFICE-WORKSPACE-OVERVIEW-002
  - REQ-OFFICE-WORKSPACE-OVERVIEW-003
system_design:
  - ../../specs/office/system-design/workspace-overview.md
---

# Implementation Plan: Office Workspace Overview

## Overview

Office users can review several Office workspaces on one page. The page reads the caller's workspace list, shows task and agent counts, and merges recent activity. It adds no write path and does not change workspace access.

The backend guard rejects agent tokens. The frontend stores the current response in memory and refreshes it every 30 seconds. Workspace and activity links keep the workspace context.

## UI preview

`UI-01` shows the loaded overview at `/office/overview`. The Office shell provides navigation and the page title. The content scrolls below the shell.

Desktop:

```text
Office navigation | Overview
                  | Workspace A  Tasks 12  Tasks (In progress) 2  Done 8  Agents 3  Agents (Running) 0  Approvals 1
                  | Workspace B  Tasks  4  Tasks (In progress) 1  Done 2  Agents 1  Agents (Running) 1  Approvals 0
                  | Recent activity
                  | Task KAN-21 updated in Workspace A        [Run]
                  | Agent completed KAN-18 in Workspace B      [Run]
```

Phone:

```text
Office menu
Overview
Workspace A
Tasks 12 · Tasks (In progress) 2 · Done 8
Agents 3 · Agents (Running) 0 · Approvals 1
Workspace B
Tasks 4 · Tasks (In progress) 1 · Done 2
Agents 1 · Agents (Running) 1 · Approvals 0
Recent activity
Task KAN-21 updated in Workspace A       [Run]
Agent completed KAN-18 in Workspace B    [Run]
```

The phone layout stacks the same workspace cards and activity rows. Each row remains a separate touch target. The counts and navigation are required; spacing is illustrative. The labels distinguish task workflow state from Office agents that are currently working; a task can remain in progress with no executing session.

## Work order

- [Task 01: Add the read-only workspace overview](task-01-cross-workspace-overview.md)
- [Task 02: Extend scope, sections, and lazy lists](task-02-overview-sections.md)

## Expanded overview preview

`UI-02` extends the loaded page with an Office-only or reachable-workspace scope picker. Six system cards show uptime, active tasks, running AI sessions, queued messages, human-action items, and blocked model accounts. Task, session, and queue cards open separate inline lists. Each workspace card shows six metrics, problem counts, parent groups, and an expandable task table. Human-action items, the last 24 hours, and model health follow. On phone layouts the same sections stack vertically. Links open the workspace, exact task/session, inbox, automation, or agent profile they describe.

## Verification strategy

- Run backend tests for the aggregate service, SQLite queries, route scope, and Codex probe stderr handling.
- Run frontend unit tests for the aggregate store, hook, and activity links.
- Run the desktop and phone Office navigation browser tests.
- Run the specification linter and document catalog validator.

## Implementation result

The work order is complete. The backend and frontend now provide the read-only overview. The review fix also waits for process cleanup before it classifies managed npm probe errors.
