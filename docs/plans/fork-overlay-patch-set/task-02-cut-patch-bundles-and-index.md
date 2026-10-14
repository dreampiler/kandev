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

Cut on 2026-10-14 UTC from source `3368a2084825` (task 01 and the design
package on fork `main` `9f005901b35a`) at upstream base `51af93dcd86b`, and
committed in `3349e6c586`.

| Patch | Files | Lines |
| --- | ---: | ---: |
| `20-dynamic-tier.patch` | 82 | 11,827 |
| `21-provider-limits.patch` | 76 | 7,531 |
| `22-control-capacity.patch` | 29 | 1,717 |
| `23-peer-batch.patch` | 20 | 1,826 |
| `30-office-overview.patch` | 69 | 7,803 |
| `90-fork-remainder.patch` | 1,358 | 116,821 |
| **Total** | **1,634** | **147,525** |

The `diff --git` counts sum to 1,634, equal to the fork-delta file count from
`git diff --no-renames --numstat`, and the line totals match its +141,416 and
-6,109. Before formatting, the last bundle commit's tree equaled the source
tree. 180 files touched by a named bundle and another commit stay in the
remainder (for example, 42 D01 files and 29 D02 files shared with
unattributed commits). `patches/.gitattributes` marks the patches as generated
and hides their text diff.
