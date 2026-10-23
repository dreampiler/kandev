---
id: "01-implementation"
title: "feat(workflows): require explicit profiles before task creation"
status: in_progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WORKFLOW-AGENT-OVERRIDES-002
acceptance_criteria:
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.1
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.2
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.3
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.4
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.5
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.6
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.7
  - AC-TASKS-WORKFLOW-AGENT-OVERRIDES-002.8
system_design:
  - ../../specs/tasks/system-design/workflow-agent-overrides.md
---

# feat(workflows): require explicit profiles before task creation

## Outcome and scope

Optionally require an explicit task profile or applicable validated step override before native task creation, across UI/API/MCP and watch producers.

Out of scope: Enabling the policy by default, treating metadata/defaults as explicit choices, or changing idempotent external-ID replay.

## Implementation acceptance

1. The affected native entrypoint follows the linked requirement and design while preserving unrelated behavior and history.
2. Rejected, stale and failed operations retain their existing refusal or rollback semantics.
3. The focused implementation checks and linked documentation coverage have honest recorded results; unexecuted follow-up checks remain pending.

## Files and dependencies

The source files are the existing PR #4718 diff. This work order adds its delivery record and links existing requirements and system design; it introduces no runtime gate or permanent test.

- [System design](../../specs/tasks/system-design/workflow-agent-overrides.md)
- [Plan](plan.md)

## Verification and results

Validation on this main-based port: selected existing service/model/SQLite/MCP server/MCP handler tests passed, SQLite task-store conformance passed with race detection, sqlguard passed, and all normal commit hooks passed. The existing tool-schema fixture was updated; no new standing tests or CI were added.

Documentation-only correction: git diff --check and the existing .github/scripts/pr-docs.cjs validateCoverage entrypoint with the candidate changed paths and the linked candidate documents supplied on stdin. No GitHub status is synthesized locally.

The already prepared review follow-up is still in progress. Its remaining narrow checks and normal commit hooks must pass before that follow-up is published. The earlier published implementation checks do not establish the pending result.

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
