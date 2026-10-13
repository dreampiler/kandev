---
status: draft
system: office
requirements:
  - REQ-OFFICE-XWS-001
  - REQ-OFFICE-XWS-002
  - REQ-OFFICE-XWS-003
  - REQ-OFFICE-XWS-004
  - REQ-OFFICE-XWS-005
---

# Office: Cross-Workspace Access System Design

## Purpose and boundaries

One package, `internal/office/crossworkspace`, owns the setting and every reach
decision. Each surface that crosses a workspace boundary asks it; none keeps a
reach rule of its own. The package adds one table and no background job; the
completion report is an ordinary event subscriber.

It does not change the handoff action, the workspace coordinator, the
organization model, or any write path other than message delivery to an Office
task.

## Requirement mapping

| Requirement | Mechanism |
|---|---|
| `REQ-OFFICE-XWS-001` | `office_workspace_cross_access` row per Office workspace; `Service.UpdateSettings` validates with the caller's identity; the dashboard settings route applies `cross_workspace_access` first and refuses agent tokens. |
| `REQ-OFFICE-XWS-002` | `Service.Reachable` / `ReachContext`, called per request by every surface below. |
| `REQ-OFFICE-XWS-003` | Runtime routes plus CLI; the `office-cross-workspace` MCP capability and the Office guard in the MCP dispatcher; the agent `GET` exception in the Office scope middleware. |
| `REQ-OFFICE-XWS-004` | `POST /runtime/tasks/:id/messages` dispatches `message_task_kandev` in-process with the run's task and session as sender. No control route exists. |
| `REQ-OFFICE-XWS-005` | `handleMessageTask` hands Office targets to `Service.DeliverToOfficeTask`; `report_to_handoff_source_kandev`; a `task.state_changed` subscriber. All post through the Office comment path that wakes the assignee. |

## Storage

```sql
CREATE TABLE office_workspace_cross_access (
  workspace_id          TEXT PRIMARY KEY,
  enabled               INTEGER NOT NULL DEFAULT 0,
  allowed_workspace_ids TEXT NOT NULL DEFAULT '[]',  -- JSON array
  coordinator_task_ids  TEXT NOT NULL DEFAULT '{}',  -- JSON object
  updated_at            TIMESTAMP NOT NULL
);
```

A missing row is the disabled default. Workspace deletion removes the row.

## Reach decision

```text
reach(office, target):
  target == office                       -> allowed
  setting(office).enabled == false        -> ErrDisabled
  allow-list non-empty and target not in  -> ErrNotReachable
  owner(office) lacks workspace.read      -> ErrNotReachable
  otherwise                               -> allowed, with ctx = owner identity
```

The owner check installs the Office workspace owner's real identity
(`mcpscope.Resolver.ScopeWorkspaceOwner`, the workspace analogue of
`ScopeOverridingIdentity`) and calls the task service's
`AuthorizeWorkspaceScope(ctx, target, workspace.read)`. That one call carries
the whole authorization model: ownership, unit and member roles, and the
organization boundary when organizations are on. With authentication disabled
the resolver leaves the context alone and the task service treats the caller as
unscoped, so reach is every workspace. The setting therefore does not depend on
the organizations flag.

Reads made on the agent's behalf run under the returned owner context, so the
Kanban handlers apply the same identity again.

## Office agent surfaces

**Runtime routes** (under `/api/v1/office`, workspace-less like every
`/runtime/*` route; the handler derives the Office workspace from the run token):

| Route | Answer |
|---|---|
| `GET /runtime/workspaces` | `{cross_workspace_access_enabled, workspaces:[{id,name,self,is_office,coordinator}]}` |
| `GET /runtime/workspaces/:id/coordinator` | `{coordinator:{workspace_id,task_id,task_title,source}}` |
| `GET /runtime/workspaces/:id/tasks[?workflow_id=]` | `list_tasks_kandev` output for one or every workflow |
| `GET /runtime/tasks/:id/sessions` | `list_task_sessions_kandev` output |
| `GET /runtime/tasks/:id/conversation` | `get_task_conversation_kandev` output |
| `GET /runtime/tasks/:id/plan` | `get_task_plan_kandev` output |
| `POST /runtime/tasks/:id/messages` | `message_task_kandev` result |

Reads need the run's `list_tasks` capability and the send needs `post_comment`,
both default-on for every role. `ErrDisabled` answers `403` and
`ErrNotReachable` `404`. The routes are served by
`officeruntime.CrossWorkspaceGateway`, implemented in `internal/backendapp`
because it needs both the Office runtime and the MCP dispatcher: it decides
reach, then dispatches the named MCP action in-process through
`mcpserver.DispatcherBackendClient`, so an Office agent gets byte-for-byte what a
Kanban agent gets and a send keeps queueing, sender attribution, and
`operation_id` replay.

**CLI:** `kandev workspaces list|coordinator|tasks`, `kandev tasks list
--workspace`, `kandev tasks sessions|transcript|plan|send`. The
`kandev-task-ops` system skill documents them in
`references/cross-workspace.md`.

**MCP:** at session launch the executor adds
`CapabilityOfficeCrossWorkspace` to an Office task profile when the setting is
on, which registers `list_workspaces_kandev`, `list_workflows_kandev`,
`list_workflow_steps_kandev`, `list_tasks_kandev`,
`get_task_conversation_kandev`, `list_task_sessions_kandev`, and
`message_task_kandev`. Discovery is not the boundary: the guarded MCP dispatcher
runs `authorizeOfficeCrossWorkspaceRequest` for every Office-surface principal,
resolving each action's target workspace from `workspace_id`, `workflow_id`, or
`task_id` and refusing it out of reach. `list_workspaces_kandev` from an Office
principal answers the reach directory instead of every visible workspace.
`get_task_plan_kandev`, `list_task_plan_revisions_kandev`, and
`get_task_plan_revision_kandev` are already on the Office surface; the guard now
evaluates each one's `task_id`, so a reached workspace's plan is readable and an
out-of-reach one is refused. `list_tasks_kandev` also resolves its target from
`workspace_id` when that is supplied instead of `workflow_id`. Plan writes,
`move_task_kandev`, archive, and delete stay out of the guarded set: the target
workspace's coordinator owns control actions (REQ-OFFICE-XWS-004.3).

**Office routes:** `authorizeOfficeAgentCallerWithReach` lets a `GET` whose ids
all resolve to one reached workspace through. With `reach == nil` or any other
method it is the original confinement.

## Coordinator resolution

For a reached workspace: the `coordinator_task_ids` entry when it names a live,
unarchived task of that workspace (`setting`); else the first coordinator of the
workspace that has a conversation task, when the coordinator feature is on
(`workspace_coordinator`); else `none`. Resolution is read-only and never
creates anything.

## Return path

- `handleMessageTask` calls `DeliverToOfficeTask` after resolving the target and
  before any session lookup, skipping parent-question replies. It acts only when
  the target is an Office task, its workspace has the setting on, and that
  workspace reaches the sender's workspace. It posts through
  `DashboardService.CreateComment`, the path runtime agent comments use, so the
  workflow engine's `on_comment` trigger queues the assignee's `task_comment`
  run.
- `report_to_handoff_source_kandev` is registered on Kanban sessions with
  `CapabilityOfficeHandoffReport`, which the executor adds when some Office
  workspace with the setting on reaches the session's workspace. The backend
  takes the caller workspace from the MCP principal, never the payload.
- `SubscribeHandoffCompletion` listens to `task.state_changed`; on a terminal
  state it reads `handoff_source`, re-decides reach from the source Office
  workspace, and posts the report asynchronously so the state change is never
  delayed.

Comments carry `source = cross_workspace` and an empty `author_id`. The
self-comment rules compare `author_id` with the assignee, so they never match,
and the channel relay only relays comments with a reply channel, so none is
relayed. The Office comment prompt names the author "another workspace's task".

## Failure behavior

- A settings read failure makes `Enabled` false and the reach decision return an
  error: the feature fails closed.
- An owner whose account is disabled or missing under enforced authentication
  makes reach fail closed (`ErrNotReachable`).
- A completion report failure is logged and dropped; the coordinator can still
  report by hand.

## Compatibility

Default off in every profile. With the setting off: the Office MCP catalog is
unchanged, the new runtime routes answer for the own workspace only, the Office
scope middleware is unchanged, `message_task_kandev` to an Office task is
delivered to its session as before, and no completion report is posted. No
runtime flag is added because the per-workspace setting is itself the opt-in
and kill switch.
