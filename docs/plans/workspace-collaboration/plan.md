---
created: 2026-10-15
status: in-progress
requirements:
  - REQ-TASKS-WORKSPACE-COLLABORATION-001
system_design:
  - ../../specs/tasks/system-design/workspace-collaboration.md
legacy_specs: []
---

# Workspace collaboration first server slice

Implement [Task 01](task-01-conversation-grants.md) in the independent upstream
worktree. Preserve the disabled native path, existing task/session history and
native permission resolver. Use server stream identity, a task-owned typed grant
store, composition callbacks, independent action enforcement and dedicated
non-prompting report transport. Add the off-by-default typed native toggle to all
registry/profile/frontend contracts. Verification is disposable and bounded to
the changed paths. Git publication and operating activation belong to integration.

Profile inheritance, taskless callers, delegated authority, the grant editor and
aggregate navigation remain explicit original follow-up scope, not completed
behavior or prerequisites for this slice.

The [transport coverage](../../specs/tasks/system-design/workspace-collaboration.md#transport-coverage-and-original-remaining-work)
records the generic plugin Host callback's missing verified conversation origin.
This slice is an API/domain foundation and does not claim to enforce grants across
the main MCP plugin-tool flow until that origin is propagated and verified.
