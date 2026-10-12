---
status: draft
system: office
created: 2026-10-12
owners:
  - kandev
---

# Office: Cross-Workspace Access Requirements

> Upstream proposal candidate. This is an opt-in extension that is off by
> default, so an install that never turns it on behaves exactly as before.
> It departs on purpose from two current upstream rules, named under
> `## Departures from current rules`.

## Overview

People who run one Kanban workspace per project and one Office workspace to
manage them cannot ask the Office agent "what is blocked across my projects?"
today. An Office agent's runtime token is pinned to its own workspace, and the
one cross-workspace write it has, the handoff, only goes one way: work that an
Office agent hands off never reports back.

This capability adds one Office workspace setting, **cross-workspace access**.
When an operator turns it on, the Office workspace's agents can read other
workspaces and send messages to tasks there. Control (stopping, moving, and
re-prioritizing tasks) stays with each workspace's own coordinator: the Office
agent asks, and the coordinator acts. Replies and handed-off results come back
to the Office task as comments, which wake its assignee.

## Terms

- **Office workspace:** the workspace that holds the setting and whose agents
  use it.
- **Target workspace:** any other workspace an Office agent reads or messages.
- **Reach:** the set of target workspaces the Office workspace's agents may use.
- **Coordinator task:** the task an Office agent should address in a target
  workspace: a task the operator names in the setting, or the target
  workspace's coordinator conversation (`docs/specs/coordinator/`).

## Departures from current rules

- An Office agent token is confined to the workspace it was minted for
  (`internal/backendapp/office_scope.go`, `authorizeOfficeAgentCaller`). With the
  setting on, reads of a reached workspace are an explicit exception; writes
  stay confined.
- The organization-scoped coordinator proposal (kdlbs/kandev#4330) computes reach
  as the workspaces in the owner's organization on which the owner holds
  `workspace.read`, and excludes Office workspaces. This capability uses the
  same reach formula with an Office workspace as the source, which that
  proposal excluded.

## Requirements

### REQ-OFFICE-XWS-001: An Office workspace setting turns cross-workspace access on

**User story:** As an operator who manages several project workspaces from one
Office workspace, I want to let its agents work across those workspaces, so
that I can ask one agent about all of them.

#### Acceptance criteria

- **AC-OFFICE-XWS-001.1:** Every Office workspace shall have a cross-workspace
  access setting with three parts: `enabled` (default `false`),
  `allowed_workspace_ids` (default empty), and `coordinator_task_ids` (a map from
  a target workspace id to a task id, default empty).
- **AC-OFFICE-XWS-001.2:** When `enabled` is `false`, every Office agent surface
  shall behave exactly as it does without the setting.
- **AC-OFFICE-XWS-001.3:** The setting shall be read and written through the
  Office workspace settings API (`GET`/`PATCH
  /api/v1/office/workspaces/:wsId/settings`, field `cross_workspace_access`) and
  shown in the Office workspace settings page as a toggle, an allow-list of the
  other workspaces, and an optional coordinator task id per workspace.
- **AC-OFFICE-XWS-001.4:** When an Office agent token sends a `PATCH` that
  contains `cross_workspace_access`, the system shall refuse it with `403` and
  apply no field of that request. An agent never widens its own reach.
- **AC-OFFICE-XWS-001.5:** When an update names the Office workspace itself, a
  workspace the person cannot read, or a coordinator task that does not belong
  to the workspace it is mapped to, the system shall refuse the update with
  `400` and apply no field of that request.

### REQ-OFFICE-XWS-002: Reach is computed on every call

#### Acceptance criteria

- **AC-OFFICE-XWS-002.1:** A target workspace shall be in reach when, and only
  when, the setting is on, the target is in `allowed_workspace_ids` or that list
  is empty, and the Office workspace's owner holds `workspace.read` on the
  target. With organizations on, the owner read check includes the organization
  boundary; with authentication off, it admits every workspace.
- **AC-OFFICE-XWS-002.2:** The Office workspace itself shall always be in reach.
- **AC-OFFICE-XWS-002.3:** Reach shall be decided at the time of each call. A
  session that started while the setting was on shall be refused once the
  setting is turned off.
- **AC-OFFICE-XWS-002.4:** A refusal shall not tell a target that is out of reach
  apart from one that does not exist. A refusal because the setting is off may
  say so.

### REQ-OFFICE-XWS-003: Office agents read reached workspaces

#### Acceptance criteria

- **AC-OFFICE-XWS-003.1:** An Office agent shall be able to list the workspaces
  in reach, each with its coordinator task and the source of that answer
  (`setting`, `workspace_coordinator`, or `none`).
- **AC-OFFICE-XWS-003.2:** For a workspace in reach, an Office agent shall be
  able to list its tasks (optionally for one workflow), and for a task in reach,
  read its sessions, its conversation, and its plan, receiving the same data a
  Kanban task agent's tools return.
- **AC-OFFICE-XWS-003.3:** These reads shall be available through the Office
  agent CLI (`$KANDEV_CLI kandev workspaces …` and `kandev tasks
  sessions|transcript|plan|list --workspace`) and, for sessions started while the
  setting is on, through MCP tools of the same names Kanban agents use.
- **AC-OFFICE-XWS-003.4:** With the setting on, an Office agent token may also
  `GET` an Office resource route whose ids all resolve to one reached workspace.
  Any other method stays confined to the token's own workspace.

### REQ-OFFICE-XWS-004: Office agents ask, coordinators act

#### Acceptance criteria

- **AC-OFFICE-XWS-004.1:** An Office agent in a task-scoped run shall be able to
  send a message to a task in reach. The message shall be delivered as
  `message_task_kandev` delivers it, attributed to the Office task, with an
  instruction to reply to that task.
- **AC-OFFICE-XWS-004.2:** A taskless Office run shall be refused, because a
  reply needs an Office task to land on.
- **AC-OFFICE-XWS-004.3:** The capability shall add no way for an Office agent to
  stop, move, re-prioritize, archive, or delete a task in another workspace.

### REQ-OFFICE-XWS-005: Replies and results come back to the Office task

#### Acceptance criteria

- **AC-OFFICE-XWS-005.1:** When a task sends `message_task_kandev` to an Office
  task whose Office workspace has the setting on and reaches the sender's
  workspace, the message shall become a comment on the Office task that wakes
  its assignee, and the send shall report status `commented`. Otherwise the
  message shall be delivered as before.
- **AC-OFFICE-XWS-005.2:** A Kanban task session whose workspace is reached by
  an Office workspace with the setting on shall have
  `report_to_handoff_source_kandev`, which posts a report on the Office task
  recorded in a handed-off task's `handoff_source`, waking its assignee. The
  caller shall only report on tasks of its own workspace.
- **AC-OFFICE-XWS-005.3:** When a handed-off task reaches a terminal state
  (`COMPLETED`, `FAILED`, or `CANCELLED`) and the source Office workspace has the
  setting on and reaches the task's workspace, the system shall post a comment
  on the source Office task that names the task and its state.
- **AC-OFFICE-XWS-005.4:** These comments shall carry source `cross_workspace` and
  no Office agent author, so a comment never counts as the assignee's own and
  is never relayed to an external channel.

## Out of scope

- Writing into another workspace other than by message: creating tasks there
  stays the existing handoff action.
- Per-action permission settings for what a coordinator may do on an Office
  agent's request. The coordinator's own surface decides.
- An organization layer above workspaces. The setting is per Office workspace.
