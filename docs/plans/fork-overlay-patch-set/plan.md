---
created: 2026-10-15
updated: 2026-10-15
status: in_progress
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-001
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-002
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-003
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-004
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-005
system_design:
  - ../../specs/release/system-design/fork-overlay-patch-set.md
legacy_specs: []
---

# Implementation Plan: Fork overlay patch set (migration stage M1)

## Overview

This plan moves the fork from history-only local changes to an overlay:
upstream base plus ordered `patches/*.patch`, indexed by `patches/INDEX.md`.
It implements
[the requirements](../../specs/release/requirements/fork-overlay-patch-set.md)
through [the system design](../../specs/release/system-design/fork-overlay-patch-set.md).

The migration stage completes only when all three results hold:

1. A fresh `kd/overlay` reproduces the source with `git am` in index order.
2. `main` adopts the overlay through a history-preserving merge commit.
3. One quoted success log of the sync automation, and one installed
   `GET /api/v1/system/info` readback matching the verified overlay revision.

Tasks 01 to 03 need no further authorization and land in one fork PR. Tasks 04
and 05 need separate authorization and are recorded here so the stage is not
reported as complete early.

## Measured baseline (2026-10-14 UTC)

Measured after `git fetch origin --prune` and `git fetch upstream main`. The
earlier inventory counts (23 upstream-identical commits, 171 test-residue
lines, 10 duplicate commits) are design inputs, not these measurements.

| Item | Value |
| --- | --- |
| Fork `origin/main` | `9f005901b35ae0f7ddea43f2b77d89ea14721d08` |
| Upstream `upstream/main` | `73c5ddb8c9550744655432d03edbe5872b6bbb80` |
| Upstream base (merge base) | `51af93dcd86beb66f15d0495a92bb6c846ff2177` |
| Upstream commits not yet in the fork | 31 |
| Fork commits not in upstream | 482 raw, 350 non-merge, 311 first-parent |
| Fork delta from the base | 1625 files, +140635/-6108 lines |

## Dependency order

| Wave | Work order | Depends on | Authorization |
| --- | --- | --- | --- |
| 1 | [Task 01: Remove verified upstream duplicates](task-01-remove-verified-upstream-duplicates.md) | none | this PR |
| 2 | [Task 02: Cut the patch bundles and index](task-02-cut-patch-bundles-and-index.md) | 01 | this PR |
| 3 | [Task 03: Reproduce the overlay with git am](task-03-reproduce-overlay.md) | 02 | this PR |
| 4 | [Task 04: Adopt the overlay in main](task-04-adopt-overlay-in-main.md) | 03, PR merged | owner approval for the `main` merge |
| 5 | [Task 05: Sync automation handoff and readback](task-05-sync-automation-handoff-and-readback.md) | 04 | CM owner change and an approved installation |

## Verification strategy

Use the narrowest check that proves each result. Do not run the full test
suite, a full build, repeated runs, or a base-branch comparison rerun.

- Task 01: run the changed package's tests once.
- Task 02: check the index against the generated patch files.
- Task 03: apply the patches to a fresh `kd/overlay` once and compare trees.
- Tasks 04 and 05: one ancestry check and one installed readback after the
  authorized actions.

No new recurring test, gate, CI job, ledger, or canary is added.

## Risks

- **Drift:** `main` moves while the PR is open. The index names its source
  revision; the next sync regenerates the patch set.
- **Large remainder:** most of the delta stays in the remainder bundle until a
  later classification attributes shared files.
- **Open upstream PRs:** #4045, #4428, #4429, #4430, #4445, #4448, #4454, and
  #4461 overlap fork code. Their merge changes a bundle's state, not this cut.
- **Landing method:** fork PRs are squash-merged. The task 04 merge commit
  needs the merge-commit landing path instead.
- **Ownership:** the sync automation is CM-owned; this repository only fixes
  the procedure it follows.

## Exclusions

- Later migration stages M2 to M11.
- Operating deployment, installation swaps, and CM changes in tasks 01 to 03.
- Upstream GitHub writes.
- Force push, reset, history deletion, or branch rename.
