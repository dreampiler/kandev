---
status: current
system: tasks
requirements:
  - REQ-TASKS-WORKSPACE-COLLABORATION-001
---

# Conversation collaboration boundary

## Identity, authority and storage

Backend composition reads the trusted `mcp/scope.Principal` derived from the
execution's task/session. Task services receive a narrow callback interface and
do not import the MCP transport. Enabled checks run even when native auth is
disabled; real user identity still passes through native workspace authorization.
Browser requests and internal work without a conversation principal remain native.
An execution context lacking its expected principal is an enforcement error.

The task repository owns a typed `workspace_collaboration_grants` table, keyed by
caller task, current source workspace and target workspace. The four independent
booleans default to false. Caller-session IDs are validated on every operation
but are not grant keys. Moving the task does not silently carry authority into a
new source workspace. General task metadata updates cannot change these rows.
Explicit all-false updates revoke authority without deleting the saved record.
The table is initialized after core task migrations. Foreign keys cascade on
caller-task, source-workspace or target-workspace deletion in SQLite and
PostgreSQL, including direct deletes and transactional workspace cascades. This
also covers existing reset deletion paths without separate integration cleanup.
Archival, session replacement and explicitly turning the feature off retain grants.

Human grant management requires native `workspace.manage` in the source workspace
and native read reach of the target. Conversation callers cannot edit grants,
including their own. Each exercised action separately intersects native target
authorization. Existing org, capability and coordinator restrictions remain.

## Action mapping

| Grant | Native operations in this slice | Native permission also required |
| --- | --- | --- |
| read | task/session/workspace reads and lists at the task-domain boundary | workspace.read |
| report | dedicated append-only status report on an existing target session | task.write |
| prompt | ordinary message admission and start/resume prompt authorization | session.prompt, or existing coordinator manager scope |
| control | task/workflow mutations, session stop/control/exec and workspace administration at the same boundary | existing corresponding native scope |

Report entry points accept only target task/session and text. Server code fixes
the status-message type, author and completed-turn behavior. They never call
orchestrator prompt/queue or workflow mutation. Generic message requests still
require prompt permission even if their body claims an agent author.
The report primitive requires a verified conversation context; a plain human
request cannot fabricate an agent-authored report by supplying caller fields.

## Feature lifecycle and API

`features.workspaceCollaboration` uses
`KANDEV_FEATURES_WORKSPACE_COLLABORATION`, is off in prod/dev/e2e and requires
restart. The existing typed runtime registry, persisted overrides and System
Feature Toggles UI own it. Explicit environment > override > profile still holds.
Off closes the new grant/report APIs and preserves existing native APIs. Explicit
administrator off restores native behavior without deleting saved grants.
An enabled store/identity failure returns an error and never takes this off path.

The native API reads and replaces grants under
`/api/v1/tasks/:id/workspace-collaboration`; reports have a separate task/session
entry point. Server route identity determines the grantee; a body cannot replace
the caller task, session, source workspace or user identity. The current grant API
is the first usable settings surface; a full per-conversation editor is follow-up.

## Settings UI composition

```text
Settings > System > Feature Toggles
  Workspace collaboration                         [ OFF / ON ]
  Restart required. Off restores native permissions; saved grants remain.

Conversation > Workspace collaboration (subsequent editor)
  Source: [logical conversation] in [current workspace]
  Target workspace        Read  Report  Prompt  Control
  [workspace selector]    [ ]   [ ]     [ ]     [ ]
  Native user permissions still apply.             [Save changes]
  Load/save failure: preserve draft; show error; do not broaden permissions.
```

## Verification

Use existing native authorization and typed flag checks plus disposable SQLite
and dispatcher overlays. Exercise false/default grants, independent actions,
same-workspace/native browser behavior, session replacement, spoofed request
identity, native RBAC denial, store failure, human-only edits and report isolation.
Fresh/replayed schema initialization must preserve grants and existing records.
No new poller, queue, permanent test framework or operating ACL update is needed.

## Transport coverage and original remaining work

The implemented boundary consumes trusted conversation contexts at task-domain
entry points. The native in-session stream installs those contexts through
`agent/runtime/lifecycle/mcp_identity.go` and `mcp/scope/principal.go`; ordinary
human HTTP/WS and internal scheduler calls remain native. The new report primitive
is an HTTP/WS API, not yet an advertised MCP tool or full grant-editor UI.

The generic plugin Host return path is an explicit remaining gap. The server
passes `AgentToolInvocationContext` into a plugin tool RPC, but
`pkg/pluginsdk/serve.go` constructs one long-lived broker Host service and
`pkg/pluginsdk/host.go` forwards each incoming Host RPC context independently.
The current callback carries plugin capability authority, without restoring the
originating conversation principal. Caller task/session fields in tool/request
JSON are not sufficient evidence to restore that authority. No such payload is
trusted by this implementation. A generic Host call without verified conversation
origin therefore remains governed by existing plugin/native permissions; it is
not covered by this conversation grant boundary and may bypass its narrowing.

The main MCP plugin-tool flow cannot be described as protected until a separate
bounded origin-propagation change connects the actual broker callback, owner
identity and action to this boundary while preserving non-agent plugin work.
This first API/domain slice does not complete the overall workspace-collaboration
permission goal. It does not solve that gap by disabling existing native APIs,
requiring plugin installation, treating payload IDs as credentials or granting
authority from watches. Profile inheritance, delegation and taskless callers also
remain original follow-up work. Aggregate navigation is independent of this gap.
