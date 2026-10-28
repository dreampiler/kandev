---
created: 2026-10-29
status: complete
requirements:
  - REQ-WORKSPACES-WORKTREE-BASE-REFRESH-001
system_design:
  - ../../specs/workspaces/system-design/worktree-base-refresh.md
legacy_specs: []
---

# Implementation Plan: Repository version monotonicity

## Overview

Repository saves use `updated_at` as the optimistic-concurrency version token.
Two stale saves that observed the same future-dated version could both write
`observed + 1us`, so the second save did not advance the token, and the exact
and complete writers could rewind a future version with `time.Now()`. This plan
advances every repository version from the committed row, read under the same
transaction write lock, while keeping the exact `ExpectedUpdatedAt` race
rejection, the checkout intent, and existing field and event behavior.

## Scope

- Ordinary checkout-intent writes, the exact and legacy complete writers, and
  the per-dialect row lock used to read the stored version.
- Existing storage and service regressions only; no schema, public field, or
  event-ordering change. Other repository writers remain outside this plan.

## Work orders

- [x] [Task 01: Advance repository versions from the stored row](task-01-repository-version-floor.md) (done)

## Verification

- Focused SQLite storage and service regressions for overlapping and
  future-dated saves, plus an env-gated PostgreSQL twin.
- Changed-delta formal review on the implementation head; the same head is not
  retested without new code or failure evidence.
