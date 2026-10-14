---
status: draft
system: tasks
requirements:
  - REQ-TASKS-MCP-CTRL-001
---

# MCP: Controller Relation-Transcending Access System Design

## Discriminator

`HandoffService.IsWorkspaceController` reports whether a task is a workspace
controller: a root task (`parent_id == ""`) with at least one non-archived child.
`Handlers.controllerMayReadRelated` evaluates the server-derived principal
(`mcpscope.PrincipalFromContext`) and requires the target task to share the
principal's workspace. Both reads fail closed: any lookup error, a missing task,
or an empty workspace yields "not a controller" / "not reachable".

## Gate change

`handleListRelatedTasks` routes through `Handlers.listRelatedForCaller`. When the
caller and target differ and the principal is a controller with an in-workspace
target, it calls `HandoffService.ListRelated` (the ungated projection). Every
other case keeps `HandoffService.ListRelatedForCaller`, whose relation/workspace
gate is unchanged. The exemption is read-only and does not touch the document
tools or the plan/step handlers.

## Boundaries

- The principal identity comes from the dispatch context (the session's own
  task), never from the request payload, so `caller_task_id` cannot widen scope.
- The target must be in the principal's workspace; cross-workspace reads stay on
  the Office reach path.
- A non-controller principal keeps the existing `ErrAccessDenied` ->
  `FORBIDDEN` mapping.
