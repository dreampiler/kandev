---
status: in_progress
created: 2026-10-23
requirements:
  - REQ-TASKS-WORKFLOW-AGENT-OVERRIDES-002
system_design:
  - ../../specs/tasks/system-design/workflow-agent-overrides.md
---

# feat(workflows): require explicit profiles before task creation

Optionally require an explicit task profile or applicable validated step override before native task creation, across UI/API/MCP and watch producers.

## Delivery order and dependencies

The implementation is one existing PR slice. Preserve its native dependencies and existing feature defaults, then verify the narrow affected paths. No dependent PR is imported into this package.

- [Implementation and verification](task-01-implementation.md)

## Scope and risks

Enabling the policy by default, treating metadata/defaults as explicit choices, or changing idempotent external-ID replay. These remain outside this delivery slice. Preserve existing files, configuration and history; report unavailable integration coverage without inventing a successful result.

## Verification strategy

Reuse the completed focused implementation checks recorded below and the existing documentation-coverage evaluator. Only remaining review fixes require their affected checks to run again.

## ASCII UI preview

UI-01: Existing affected surface, after this change.

```text
Workflow settings
  [ ] Require an explicit agent profile

Create task
  Agent profile: [Choose profile]
  [Create]

Existing desktop and phone forms share the optional policy and validation.
```

This sketch records the affected structure; spacing is illustrative. It does not add a new control or approval requirement.
