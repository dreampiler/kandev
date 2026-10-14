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
`handleListWorkspaceTasks`, which picks the repository contract from the caller.
A controller principal (`Handlers.callerIsWorkspaceController`, the
`REQ-TASKS-MCP-CTRL-001` discriminator) reads through
`Service.ListTasksByWorkspaceIncludingAutomation(workspaceID, "", ...)`; every
other principal reads `Service.ListTasksByWorkspace`, the provenance-hiding
contract, so automation-run rows stay out of an ordinary worker's view. Both
contracts already include workflow-less tasks, and the services and their
`sqlite.Repository` counterparts share one private implementation differing only
in the automation-origin filter, so the user-facing list keeps hiding automation
runs. `handleListWorkspaceTasks` refuses a non-Office caller that names another
workspace; the service's own workspace authorization still applies underneath.
Cross-workspace listing stays on the Office reach path, which this change does
not touch. The workspace form is a single page of up to `workspaceTaskListPageSize`
(200) tasks in the server's default order; `total` carries the full count and the
tool description states the bound.

## Failure behavior

- A failed prefix lookup logs and leaves the raw value, so the tool's own
  not-found path answers; resolution never fails a request that the target tool
  would otherwise handle.
- Workspace authorization keeps its existing failure modes; the new reads add no
  new grant.
