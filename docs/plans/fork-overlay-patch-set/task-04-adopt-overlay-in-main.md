---
id: task-04
title: Adopt the overlay in main
status: pending
wave: 4
depends_on:
  - task-03
plan: plan.md
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-004
acceptance_criteria:
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-004.1
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-004.2
system_design: ../../specs/release/system-design/fork-overlay-patch-set.md
---

# Task 04: Adopt the overlay in main

## Outcome

Fork `main` records the overlay as a merged parent without losing any
existing commit.

## Authorization

Requires explicit owner approval for the `main` merge. The merge lands only
through the authorized gate or broker landing path with a merge-commit
method. The fork repository allows merge commits. Do not start without that
approval.

## In scope

- Regenerate the patch set from the current `main` if it moved after the
  task 02 cut, and reproduce it once as in task 03.
- Create the merge commit described in
  [Adopting the overlay in main](../../specs/release/system-design/fork-overlay-patch-set.md#adopting-the-overlay-in-main):
  first parent `main`, second parent `kd/overlay`, tree equal to `main`.
- Land it through the authorized path.

## Exclusions

- Squash merge, fast-forward, or tree-only replacement.
- Force push, reset, history deletion, or branch rename.

## Acceptance conditions

1. `git merge-base --is-ancestor <previous main> origin/main` is true.
2. `git merge-base --is-ancestor kd/overlay origin/main` is true.
3. The landed tree equals the previous `main` tree.

## Verification

```sh
git fetch origin
git merge-base --is-ancestor <previous main> origin/main
git merge-base --is-ancestor kd/overlay origin/main
git diff --quiet <previous main> origin/main
```

## Likely files

- None; the result is one merge commit.

## Results

Pending owner approval.
