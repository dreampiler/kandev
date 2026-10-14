---
id: task-06
title: Apply the patch set on the observed upstream main
status: pending
wave: 4
depends_on:
  - task-03
plan: plan.md
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-001
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-003
acceptance_criteria:
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.1
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.2
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.4
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-003.1
system_design: ../../specs/release/system-design/fork-overlay-patch-set.md
---

# Task 06: Apply the patch set on the observed upstream main

## Outcome

The patch set applies with `git am` on the `upstream/main` commit observed at
fetch, which completes the reproduction condition of the migration stage.

## Why this exists

Task 03 reproduced the cut at its merge base `51af93dcd86b`. On the observed
`upstream/main` `73c5ddb8c955`, bundle 21 stopped with five conflicts from
upstream #4448 (provider account-block hold), merged after that base.

## In scope

- Start after fork `main` has merged the observed `upstream/main` through the
  existing fork sync path, so the next cut's merge base is that commit. This
  is the "sync upstream once, then remove duplicates, then cut" order of the
  migration design.
- Re-compare fork #239 and #288 with upstream #4448, and D01 with #208 if
  #4045 has merged. Remove only verified duplicates, as in task 01.
- Cut the patch set again from the synced `main` and reproduce it once on a
  fresh `kd/overlay` at the observed `upstream/main`.

## Exclusions

- The upstream sync merge itself, which is its own fork PR.
- Changes to `main` history, upstream writes, and operating changes.

## Acceptance conditions

1. The index base equals the observed `upstream/main`.
2. Every patch applies with the streamed command in `patches/INDEX.md`, and
   the overlay tree equals the new source outside `patches/`.

## Verification

```sh
git merge-base <source> upstream/main
git rev-parse upstream/main
```

Both commands print the same commit. Then run the streamed command in
`patches/INDEX.md` and its `git diff --quiet` check once.

## Likely files

- `patches/*.patch`, `patches/INDEX.md`

## Results

Pending the fork upstream sync.
