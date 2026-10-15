---
id: task-02
title: Cut the patch bundles and index
status: done
wave: 2
depends_on:
  - task-01
plan: plan.md
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-001
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-002
acceptance_criteria:
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.3
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-002.1
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-002.2
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-002.3
system_design: ../../specs/release/system-design/fork-overlay-patch-set.md
---

# Task 02: Cut the patch bundles and index

## Outcome

`patches/` holds one patch per bundle, starting with the large retained
bundles D02, D03, D06, D09, and D01, plus one remainder patch. `INDEX.md`
records the base, the source, the order, and each bundle's metadata.

## In scope

- Cut the patches from the source revision that includes task 01, following
  [Cutting the patch set](../../specs/release/system-design/fork-overlay-patch-set.md#cutting-the-patch-set).
- Write `patches/INDEX.md` with the fields in
  [Index fields](../../specs/release/system-design/fork-overlay-patch-set.md#index-fields).

## Exclusions

- No rewrite of fork `main` history; the original commits stay in `main`.
- No split of the remainder into further named bundles.
- No committed generation script; the cut uses the documented Git commands.

## Acceptance conditions

1. Every fork-delta file appears in exactly one patch.
2. The index rows match the generated patches' file and line counts.
3. The index names the upstream base, the observed upstream `main`, and the
   source revision.

## Verification

```sh
git diff --no-renames --numstat <base> <source> -- . ':(exclude)patches'
grep -c '^diff --git ' patches/20-dynamic-tier.patch patches/21-provider-limits.patch \
  patches/22-control-capacity.patch patches/23-peer-batch.patch \
  patches/30-office-overview.patch patches/90-fork-remainder.patch
```

The per-patch `diff --git` counts must sum to the fork-delta file count.

## Likely files

- `patches/INDEX.md`
- `patches/20-dynamic-tier.patch`, `patches/21-provider-limits.patch`,
  `patches/22-control-capacity.patch`, `patches/23-peer-batch.patch`,
  `patches/30-office-overview.patch`, `patches/90-fork-remainder.patch`

## Risks

- Attribution comes from the fixed correspondence map; commits after its
  snapshot are unattributed and keep their files in the remainder.

## Results

The initial cut on 2026-10-14 UTC was from source `3368a2084825` (task 01 and
the design package on fork `main` `9f005901b35a`) at upstream base
`51af93dcd86b`, committed in `3349e6c586`, with 1,634 files and 147,525 changed
lines.

The final cut is taken from the merged source (fork `main` `1cabfe1d` merged
into the overlay branch, plus the completion-basis document corrections), after
the fork sync stopped being a prerequisite. Its per-bundle file and line counts
and its `diff --git` totals are recorded in
[the index](../../../patches/INDEX.md#bundles). Before formatting, the last
bundle commit's tree equals the source tree. Files touched by a named bundle
and another commit stay in the remainder. `patches/.gitattributes` marks the
patches as generated and hides their text diff.
