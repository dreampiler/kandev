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
`depends_on_task_id`), only for an 8-35 character hex-and-dash prefix, and only
against the caller principal's own workspace. A value that is not such a prefix,
matches no task, or has no principal is returned unchanged; a prefix matching
several tasks is rejected with a candidate list. The prefix pattern accepts the
UUID's dashes because stored ids are dashed, and only hex and dashes can pass
it, so the value is a literal for the repository's `LIKE`.

Lookup is `Service.FindTasksByIDPrefix`, which asserts the optional narrow
`taskIDPrefixLister` repository contract rather than adding a method to the
shared `TaskRepository` interface, so the many service-test fakes stay intact.
`sqlite.Repository.ListTasksByIDPrefix` runs `workspace_id = ? AND id LIKE ?`
ordered by id and capped at 20 rows; the cap bounds an ambiguous prefix without
a second count query, so a rejection that reaches the cap reports its count as a
lower bound rather than an exact total.

## Workspace listing

`handlers.handleListTasks` branches on `workspace_id` and calls
`Service.ListTasksByWorkspaceIncludingAutomation(workspaceID, "", ...)`, whose
existing contract already includes workflow-less tasks. That service method and
its `sqlite.Repository` counterpart share one private implementation with the
provenance-hiding `ListTasksByWorkspaceWithArchiveMode`, differing only in the
automation-origin filter, so the user-facing list keeps hiding automation runs
while the control read returns them. `handleListWorkspaceTasks` refuses a
non-Office caller that names another workspace; the service's own workspace
authorization still applies underneath. Cross-workspace listing stays on the
Office reach path, which this change does not touch.

## Failure behavior

- A failed prefix lookup logs and leaves the raw value, so the tool's own
  not-found path answers; resolution never fails a request that the target tool
  would otherwise handle.
- Workspace authorization keeps its existing failure modes; the new reads add no
  new grant.
