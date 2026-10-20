---
created: 2026-10-20
status: in-progress
requirements:
  - REQ-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-008
system_design:
  - ../../specs/agents/system-design/workspace-inventory-repair.md
legacy_specs: []
---

# Implementation plan: Checkout-branch slot adoption for preserved worktrees

## Overview

A worktree task's canonical `task_environment_repos` row keys its slot by the
branch identity requested at materialization (usually the base branch). When
the task repository later gains a `checkout_branch` equal to the branch the
preserved worktree already holds, every resume and new session derives a
different slot identity and the admission guard refuses reuse with
`ErrWorkspaceReuseUnsafe`. Guarded repair cannot help for user-owned local
repositories, so the task stays unrecoverable although its checkout is intact.

## Scope

In scope: binding a worktree launch's slot identity to the single canonical
row that already holds the requested checkout branch, inside
`validateReuseEnvironmentInventory`, so resume, fresh, and additional-session
launches share it.

Out of scope: changes to guarded inventory repair, the user-owned local
repository refusal, unarchive branch restoration, and public error copy.

## Technical approach

`adoptCheckoutBranchCanonicalSlots` runs after the canonical rows load and
before slot matching. It reuses `matchingCanonicalEnvironmentRepoRows` to
prove the adopted identity resolves to exactly the holder row, and refuses
rows another requested slot already matches.

## Work orders

- [Task 01: Adopt the canonical slot of a preserved checkout branch](task-01-checkout-branch-slot-adoption.md)

## Risks

- Binding must never attach a launch to a row on a different branch, a
  deleted or failed row, or a row owned by a sibling slot; each case keeps the
  fail-closed refusal.
