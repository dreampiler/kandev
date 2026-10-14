# Fork overlay patch index

| Reference | Commit |
| --- | --- |
| Upstream base | `51af93dcd86beb66f15d0495a92bb6c846ff2177` |
| Upstream `main` at fetch (2026-10-14 UTC) | `73c5ddb8c9550744655432d03edbe5872b6bbb80` (31 commits after the base, not yet synced) |
| Source revision | `3368a2084825338545d56d2a82129ee9a0696977` |

`kd/overlay` is the upstream base plus the patches below, applied with
`git am` in this order. This file is not a patch and is never passed to
`git am`. The contract is defined in
[the system design](../docs/specs/release/system-design/fork-overlay-patch-set.md).

```sh
git switch -c kd/overlay 51af93dcd86beb66f15d0495a92bb6c846ff2177
git am --keep-cr --3way patches/20-dynamic-tier.patch patches/21-provider-limits.patch \
  patches/22-control-capacity.patch patches/23-peer-batch.patch \
  patches/30-office-overview.patch patches/90-fork-remainder.patch
git diff --quiet 3368a2084825338545d56d2a82129ee9a0696977 kd/overlay -- . ':(exclude)patches'
```

Copy the patch files out of the source branch first, because the upstream base
does not contain `patches/`.

## Bundles

Lines are added plus removed lines in the patch. A file belongs to a named
bundle only when every fork commit that touched it belongs to that bundle;
other files stay in the remainder.

| Order | Patch | Bundle | Files | Lines | Upstream relation | State | Removal condition |
| --- | --- | --- | ---: | ---: | --- | --- | --- |
| 20 | `20-dynamic-tier.patch` | D02 dynamic profile tier selection and #178 | 82 | 11,827 (+11,708/-119) | kdlbs/kandev#3811 (maintainer-owned dynamic routing, no linked PR) | open | Upstream dynamic routing extension point merged and the tier behavior moved to a plugin (E7 + PL4) |
| 21 | `21-provider-limits.patch` | D03 per-model provider limit suspension with #173, #190, #202 | 76 | 7,531 (+7,517/-14) | kdlbs/kandev#3811; general parts landed as #4320, #4328, #4331 | open | Same as 20 (E7, E7b + PL4) |
| 22 | `22-control-capacity.patch` | D06 control-session capacity and #207 | 29 | 1,717 (+1,416/-301) | kdlbs/kandev#4393 closed; no upstream control-profile concept | open | Capacity extension point upstream and the control profile moved to a plugin (E3 + PL1) |
| 23 | `23-peer-batch.patch` | D09 peer report batch delivery | 20 | 1,826 (+1,823/-3) | None | open | Delivery extension point upstream and batching moved to a plugin (E1 + PL1) |
| 30 | `30-office-overview.patch` | D01 Office overview with #174, #180, #189, #192 | 69 | 7,803 (+7,790/-13) | kdlbs/kandev#4045 open, then Organization scope (#4018 direction), then #208 | open | #4045 merged, general parts proposed upstream, operating judgments moved to a plugin (E13 + PL2) |
| 90 | `90-fork-remainder.patch` | Fork changes not yet attributed to one bundle | 1,358 | 116,821 (+111,162/-5,659) | Per commit in [the correspondence map](../docs/fork/commit-map.csv) | open | Split into named bundles as classification attributes shared files; each part follows its own condition |

**Total:** 6 patches, 1,634 files, 147,525 changed lines (+141,416/-6,109).

## Reproduction record

One run on 2026-10-14T16:30:04Z UTC with git 2.55.0.windows.3, using the
patch files committed in `3349e6c586` (the commit that added this directory):

```text
Switched to a new branch 'kd/overlay'
Applying: fork(20-dynamic-tier): D02 dynamic profile tier selection
Applying: fork(21-provider-limits): D03 per-model provider limit suspension
Applying: fork(22-control-capacity): D06 control-session capacity
Applying: fork(23-peer-batch): D09 peer report batch delivery
Applying: fork(30-office-overview): D01 Office overview
Applying: fork(90-fork-remainder): fork changes not yet attributed to one bundle
git am exit=0
cfd4f55500da1fb1b0c8052a0f329ca682988676
diff vs source exit=0
```

The resulting local `kd/overlay` head is
`cfd4f55500da1fb1b0c8052a0f329ca682988676`. Its tree equals the source
revision outside `patches/`. The branch is not pushed.

Later commits on the source branch change only `patches/` and
`docs/plans/fork-overlay-patch-set/`; they are carried by the next cut.
