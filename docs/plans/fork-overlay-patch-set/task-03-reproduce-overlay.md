---
id: task-03
title: Reproduce the overlay with git am
status: done
wave: 3
depends_on:
  - task-02
plan: plan.md
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-001
acceptance_criteria:
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.1
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.2
system_design: ../../specs/release/system-design/fork-overlay-patch-set.md
---

# Task 03: Reproduce the overlay with git am

## Outcome

A fresh `kd/overlay` at the upstream base, with every patch applied by
`git am` in index order, has the same tree as the source revision outside
`patches/`.

## In scope

- Create `kd/overlay` at the recorded upstream base.
- Apply the patches once with the command in
  [Reproducing the overlay](../../specs/release/system-design/fork-overlay-patch-set.md#reproducing-the-overlay).
- Compare the overlay tree with the source revision.

## Exclusions

- No push of `kd/overlay` and no change to `main`.
- No build or test run of the overlay; its tree equals the source revision.

## Acceptance conditions

1. Every patch applies without manual edits.
2. `git diff --quiet <source> kd/overlay -- . ':(exclude)patches'` exits 0.

## Verification

```sh
git switch -c kd/overlay 51af93dcd86beb66f15d0495a92bb6c846ff2177
git am --keep-cr --3way patches/20-dynamic-tier.patch patches/21-provider-limits.patch \
  patches/22-control-capacity.patch patches/23-peer-batch.patch \
  patches/30-office-overview.patch patches/90-fork-remainder.patch
git diff --quiet <source> kd/overlay -- . ':(exclude)patches'
```

Read the patch files from the source branch before switching, because the
upstream base does not contain `patches/`.

## Likely files

- None committed; the result is the `kd/overlay` branch and the recorded log.

## Results

One run on 2026-10-14T16:30:04Z UTC: all six patches applied with
`git am` exit 0 on a fresh local `kd/overlay`, head `cfd4f55500da`.
`git diff --quiet 3368a2084825 kd/overlay -- . ':(exclude)patches'` exited 0.
The full log is in
[the index reproduction record](../../../patches/INDEX.md#reproduction-record).
