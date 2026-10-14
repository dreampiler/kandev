---
id: task-05
title: Sync automation handoff and readback
status: pending
wave: 5
depends_on:
  - task-04
plan: plan.md
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-005
acceptance_criteria:
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-005.1
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-005.2
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-005.3
system_design: ../../specs/release/system-design/fork-overlay-patch-set.md
---

# Task 05: Sync automation handoff and readback

## Outcome

The CM owner replaces the "Daily fork upstream sync" automation prompt with
the procedure in
[Sync procedure](../../specs/release/system-design/fork-overlay-patch-set.md#sync-procedure).
One successful run is quoted, and one installed readback matches the verified
overlay revision.

## Authorization

The automation prompt and scripts are CM-owned. The change goes through the
canonical CM owner and its existing broker or promote path. The installation
used for the readback needs its own approval. This repository's tasks do not
write CM files.

## In scope

- Hand the procedure and this plan's paths to the CM owner.
- Quote one successful sync run log with the fields in
  [Observability](../../specs/release/system-design/fork-overlay-patch-set.md#observability).
- Read `GET /api/v1/system/info` once from the approved installation and
  compare its revision with the verified overlay revision.

## Exclusions

- Direct edits to CM automation, operating databases, or installed homes.
- Calling endpoints that the installed build does not ship.
- A new recurring check.

## Acceptance conditions

1. The quoted run log shows the base, the applied bundles, the index total,
   and a success outcome.
2. The installed revision equals the verified overlay revision.

## Verification

```sh
curl.exe -s http://localhost:<port>/api/v1/system/info
```

## Likely files

- `patches/INDEX.md` (state updates after the run)

## Results

Pending CM owner change and an approved installation.
