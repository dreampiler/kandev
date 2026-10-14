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

Run the streamed command in
[the index](../../../patches/INDEX.md), which reads each patch with
`git cat-file blob <patch-commit>:patches/<name>` because the upstream base
does not contain `patches/`.

## Likely files

- None committed; the result is the `kd/overlay` branch and the recorded log.

## Results

One run on 2026-10-14T16:30:04Z UTC: all six patches applied with
`git am` exit 0 on a fresh local `kd/overlay`, head `cfd4f55500da`.
`git diff --quiet 3368a2084825 kd/overlay -- . ':(exclude)patches'` exited 0.
The full log is in
[the index reproduction record](../../../patches/INDEX.md#reproduction-record).

This proves the cut at its merge base, an intermediate check under
AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.4. A second run on the observed
`upstream/main` `73c5ddb8c955` applied bundle 20 and stopped at bundle 21 with
five conflicts from upstream #4448; it was aborted. Applying on the observed
`upstream/main` moves to task 06.
