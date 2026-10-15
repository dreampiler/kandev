---
status: active
system: tasks
created: 2026-10-15
owners:
  - kandev
---

# Conversation workspace collaboration

## Requirements

### REQ-TASKS-WORKSPACE-COLLABORATION-001: Explicit conversation reach

- **AC-TASKS-WORKSPACE-COLLABORATION-001.1:** The opt-in native feature shall
  narrow verified conversation callers by target workspace and independent read,
  report, prompt and control grants, intersecting existing native permissions.
- **AC-TASKS-WORKSPACE-COLLABORATION-001.2:** Caller identity shall come from the
  server-owned task/session stream, never request identity fields. The session
  shall belong to the logical task and its current workspace.
- **AC-TASKS-WORKSPACE-COLLABORATION-001.3:** Grants shall belong to the logical
  conversation task and source workspace, survive runtime-session replacement,
  and remain outside agent-editable task metadata. Native human workspace
  managers alone shall edit them.
- **AC-TASKS-WORKSPACE-COLLABORATION-001.4:** Report-only access shall append a
  status report without prompting, changing workflow or controlling execution.
  Generic messages and mutations shall not consume report permission.
- **AC-TASKS-WORKSPACE-COLLABORATION-001.5:** Enforced grant/identity lookup errors
  shall reject the operation; they shall never disable enforcement or restore
  legacy access.
- **AC-TASKS-WORKSPACE-COLLABORATION-001.6:** All shipped defaults shall be off.
  Off shall preserve existing native API behavior and close new collaboration
  entry points. An explicit administrator change to off restores native behavior
  without deleting grants; off and enforcement failure shall be distinguishable.
- **AC-TASKS-WORKSPACE-COLLABORATION-001.7:** Same-workspace conversations,
  human browser requests and internal schedulers shall retain their native
  behavior. A grant shall never widen owner, tenant, membership or capability
  permissions or start a model merely by being edited or read.

## Scope

The first slice covers task/session conversation callers, native grant APIs,
task-domain read/prompt/control boundaries and append-only reports. Profile
inheritance, delegated authority, taskless Office principals, a full grant-editor
UI and the independent aggregate-navigation feature remain subsequent slices.
The generic plugin Host callback also needs verified conversation-origin
propagation before the main MCP plugin-tool flow satisfies this overall goal;
the first API/domain slice does not claim that coverage.

## System design

[Conversation collaboration](../system-design/workspace-collaboration.md).
