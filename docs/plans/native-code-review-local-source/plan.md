# Native Code Review Local Sources Delivery

**Status:** done

## Outcome and scope

Deliver the operator's 2026-10-13 work order: independently review gate-slot
changes through the existing task review subsystem, retain writer-model
exclusion, and read local changes when agentctl is unavailable.

Requirements: [REQ-AGENTS-NATIVE-CODE-REVIEW-003](../../specs/agents/requirements/native-code-review-local-source.md),
with the existing [reviewer exclusion contract](../../specs/agents/requirements/native-code-review-reviewer-exclusion.md).
Design: [Local Review Sources](../../specs/agents/system-design/native-code-review-local-source.md).

## Delivery order

| Work order | Status | Dependencies |
| --- | --- | --- |
| [Task 01: Local sources and fallback](task-01-local-sources.md) | done | Fork PR #257 merged before branch creation |

The primary conversation implements and integrates this single vertical slice.
Production databases and logs remain read-only. No runtime process control,
deployment, writes to the upstream repository, or changes to other lanes are
included. The operator owns later production deployment and AIWS prompt updates.

## Verification and risks

Use the changed package tests in the work order and preserve failing-before
evidence. Git reads keep the existing collection and prompt budgets; source
paths and refs are checked before a run is created. Saved sources must be
updated when the writer's gate slot changes. Local fallback admits only durable
local/worktree executors; an unknown writer still prevents review.
