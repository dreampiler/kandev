---
id: TASK-RELEASE-CONTROLLER-HANDOFF-02
title: Orchestrate approved leaves with native scheduling
status: pending
wave: 2
depends_on:
  - TASK-RELEASE-CONTROLLER-HANDOFF-01
plan: docs/plans/release-controller-handoff/plan.md
requirements:
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-001
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-002
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-003
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-007
acceptance_criteria:
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-001.1
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-001.2
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-002.1
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-002.2
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-003.1
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-003.2
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-007.2
system_design: docs/specs/release/system-design/release-controller-handoff.md
---

# Work Order 02: Orchestrate Approved Leaves with Native Scheduling

## Goal

Implement sibling-controller orchestration for approved leaves, preserving native wait/launch behavior, queue order, member order, and slot/worktree lifecycle.

## Scope

Use only interface details verified and recorded by work order 01 and the implementation owner. Do not change parent or sibling controller state.

## Acceptance

- Every write is restricted to exact approved leaf identifiers.
- Native wait/launch blocks preserve queued state/order; no manual dequeue, promotion, or synthetic launch.
- Ordered members run one at a time and advance only after success evidence and slot/worktree return are read back.
- Failure, ambiguity, absent evidence, or stale native state retains human hold.
- Fresh native state is read before resuming after wait.
- Tests cover out-of-scope identifiers, native blocking, ordering, failure retention, and slot/worktree return.

## Out of scope

Bulk operating changes, native scheduler policy changes, manual queue manipulation, and bypassing human approval gates.