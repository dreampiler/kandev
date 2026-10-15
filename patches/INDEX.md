# Fork overlay patch index

| Reference | Commit |
| --- | --- |
| Upstream base (approved fixed snapshot, merged into fork `main` by #296 `10093b3365d`) | `51af93dcd86beb66f15d0495a92bb6c846ff2177` |
| Source revision (fork `main` `1cabfe1d` merged into the overlay branch, plus the completion-basis document corrections) | `390b8daa4b0dc9521909eef423191b27ce5c6d96` |
| Upstream `main` at fetch (information only; a newer upstream is a later sync, not a prerequisite) | `8c6f4b800b123d5f074cc006a88893c807c584b9` |

`kd/overlay` is the upstream base plus the patches below, applied with
`git am` in this order. This file is not a patch and is never passed to
`git am`. The contract is defined in
[the system design](../docs/specs/release/system-design/fork-overlay-patch-set.md).

The base does not contain `patches/`, so the patches are read from the commit
that adds them (`c35a2455a187658abd2f73711d57021d741dabb3`) and streamed to
`git am` as one mailbox:

```sh
git switch -c kd/overlay 51af93dcd86beb66f15d0495a92bb6c846ff2177
P=c35a2455a187658abd2f73711d57021d741dabb3
{ git cat-file blob $P:patches/20-dynamic-tier.patch
  git cat-file blob $P:patches/21-provider-limits.patch
  git cat-file blob $P:patches/22-control-capacity.patch
  git cat-file blob $P:patches/23-peer-batch.patch
  git cat-file blob $P:patches/30-office-overview.patch
  git cat-file blob $P:patches/90-fork-remainder.patch
} | git am --keep-cr --3way
git diff --quiet 390b8daa4b0dc9521909eef423191b27ce5c6d96 kd/overlay -- . ':(exclude)patches'
```

**Completion status:** the patches reproduce the source on the approved fixed
snapshot `51af93`. Upstream #4448 and later upstream commits are outside that
snapshot; fork #239 and #288 stay until an approved sync brings their
replacement into the base. A newer upstream `main` is adopted by a later
approved sync (task 06), not before stage completion.

## Bundles

Lines are added plus removed lines in the patch. A file belongs to a named
bundle only when every fork commit that touched it belongs to that bundle;
other files stay in the remainder.

| Order | Patch | Bundle | Files | Lines | Upstream relation | State | Removal condition |
| --- | --- | --- | ---: | ---: | --- | --- | --- |
| 20 | `20-dynamic-tier.patch` | D02 dynamic profile tier selection and #178 | 77 | 11,215 (+11,096/-119) | kdlbs/kandev#3811 (maintainer-owned dynamic routing, no linked PR) | open | Upstream dynamic routing extension point merged and the tier behavior moved to a plugin (E7 + PL4) |
| 21 | `21-provider-limits.patch` | D03 per-model provider limit suspension with #173, #190, #202 | 66 | 5,662 (+5,648/-14) | kdlbs/kandev#3811; general parts landed as #4320, #4328, #4331 | open | Same as 20 (E7, E7b + PL4) |
| 22 | `22-control-capacity.patch` | D06 control-session capacity and #207 | 18 | 1,210 (+1,005/-205) | kdlbs/kandev#4393 closed; no upstream control-profile concept | open | Capacity extension point upstream and the control profile moved to a plugin (E3 + PL1) |
| 23 | `23-peer-batch.patch` | D09 peer report batch delivery | 20 | 1,826 (+1,823/-3) | None | open | Delivery extension point upstream and batching moved to a plugin (E1 + PL1) |
| 30 | `30-office-overview.patch` | D01 Office overview with #174, #180, #189, #192 | 65 | 6,400 (+6,387/-13) | kdlbs/kandev#4045 open, then Organization scope (#4018 direction), then #208 | open | #4045 merged, general parts proposed upstream, operating judgments moved to a plugin (E13 + PL2) |
| 90 | `90-fork-remainder.patch` | Fork changes not yet attributed to one bundle | 1,451 | 129,642 (+123,638/-6,004) | Per commit in [the correspondence map](../docs/fork/commit-map.csv) | open | Split into named bundles as classification attributes shared files; each part follows its own condition |

**Total:** 6 patches, 1,697 files, 155,955 changed lines (+149,597/-6,358).

## Reproduction record

One run on 2026-10-15T03:57:01Z UTC with git 2.55.0.windows.3, on a detached
checkout of `51af93dcd86b`, using the six patch files committed in
`c35a2455a18`:

```text
HEAD is now at 51af93dcd86 perf(agentctl): cut repeated background Git work in workspace polling (#4423)
Applying: fork(20-dynamic-tier): D02 dynamic profile tier selection
Applying: fork(21-provider-limits): D03 per-model provider limit suspension
Applying: fork(22-control-capacity): D06 control-session capacity
Applying: fork(23-peer-batch): D09 peer report batch delivery
Applying: fork(30-office-overview): D01 Office overview
Applying: fork(90-fork-remainder): fork changes not yet attributed to one bundle
git am exit=0
f66c0796b02b9b0409c94aa9e5f3cf3ebd5e3f8b
diff exit=0
```

The `git diff --quiet` compared the overlay with the source revision
`390b8daa4b0` outside `patches/` and exited 0.

Adoption input (no ref): merge object
`956628924fe5259f194bbc30840cef60202f2376` with parents `c35a2455a18` (the
branch head, which contains source `390b8daa4b0` plus `patches/`) and
`f66c0796b02` (the overlay). The merge tree equals the `c35a2455a18` tree, and
`c35a2455a18`, the overlay, fork `main` `1cabfe1d`, and `51af93` are all
ancestors. This is a no-ref input test; it is not the real `main` adoption.
