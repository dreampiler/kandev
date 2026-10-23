---
id: "01-implementation"
title: "feat(mcp): let direct parents reorder child tasks"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001
acceptance_criteria:
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.1
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.2
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.3
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.4
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.5
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.6
  - AC-TASKS-COORDINATOR-CHILD-TASK-ORDERING-001.7
system_design:
  - ../../specs/tasks/system-design/coordinator-child-task-ordering.md
---

# feat(mcp): let direct parents reorder child tasks

## Outcome and scope

Allow the trusted direct parent to reorder selected child tasks using existing task order and all-or-nothing membership/version checks.

Out of scope: Task hierarchy mutation, title-derived authorization, and reordering unrelated tasks.

## Implementation acceptance

1. The affected native entrypoint follows the linked requirement and design while preserving unrelated behavior and history.
2. Rejected, stale and failed operations retain their existing refusal or rollback semantics.
3. The focused implementation checks and linked documentation coverage have honest recorded results; unexecuted follow-up checks remain pending.

## Files and dependencies

The source files are the existing PR #4719 diff. This work order adds its delivery record and links existing requirements and system design; it introduces no runtime gate or permanent test.

- [System design](../../specs/tasks/system-design/coordinator-child-task-ordering.md)
- [Plan](plan.md)

## Verification and results

Validation: existing MCP, service and repository reorder selections passed. Disposable checks exercised the registered MCP → dispatcher → real SQLite → event/queued-admission path, authorization/membership rejection and the reproduced parent/workspace race before and after the correction. All normal commit hooks passed. Temporary checks were removed; no standing tests, new timer or CI were added.

Documentation-only correction: git diff --check and the existing .github/scripts/pr-docs.cjs validateCoverage entrypoint with the candidate changed paths and the linked candidate documents supplied on stdin. No GitHub status is synthesized locally.
