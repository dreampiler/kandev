---
status: draft
system: tasks
requirements:
  - REQ-TASKS-MCP-TARGET-001
  - REQ-TASKS-MCP-TARGET-002
---

# MCP: Control Task Targeting System Design

## Short-id resolution

`Handlers.expandShortTaskRefs` runs in `guardedMCPDispatcher.RegisterFunc`
before the automation, coordinator, and Office authorization guards, so every
registered action sees resolved ids. It rewrites only the scalar task-ref fields
(`task_id`, `caller_task_id`, `source_task_id`, `parent_id`,
`depends_on_task_id`), only for an 8-35 character hex prefix, and only against
the caller principal's own workspace. A value that is not a hex prefix, matches
no task, or has no principal is returned unchanged; a prefix matching several
tasks is rejected with a candidate list.

Lookup is `Service.FindTasksByIDPrefix`, which asserts the optional narrow
`taskIDPrefixLister` repository contract rather than adding a method to the
shared `TaskRepository` interface, so the many service-test fakes stay intact.
`sqlite.Repository.ListTasksByIDPrefix` runs `workspace_id = ? AND id LIKE ?`
ordered by id and capped at 20 rows; the cap bounds an ambiguous prefix without
a second count query.

## Workspace listing

`handlers.handleListTasks` branches on `workspace_id` and calls
`Service.ListTasksByWorkspace(workspaceID, "", ...)`, whose existing contract
already includes workflow-less tasks and keeps the provenance filters (archived
and automation-origin hidden). `handleListWorkspaceTasks` refuses a non-Office
caller that names another workspace; the service's own workspace authorization
still applies underneath. Cross-workspace listing stays on the Office reach
path, which this change does not touch.

## Failure behavior

- A failed prefix lookup logs and leaves the raw value, so the tool's own
  not-found path answers; resolution never fails a request that the target tool
  would otherwise handle.
- Workspace authorization keeps its existing failure modes; the new reads add no
  new grant.
