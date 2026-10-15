---
status: draft
system: release
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-001
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-002
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-003
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-004
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-005
---

# Fork Overlay Patch Set System Design

## Purpose and boundaries

The release system owns the source revision that fork releases are built
from. This design defines how that revision is expressed as an upstream base
plus ordered patches, how the patches are cut and re-applied, and how fork
`main` adopts the result.

This design uses but does not own:

- The release verification command `scripts/verify-release.ps1`, owned by the
  agents resume-turn-start package.
- The CM-owned daily sync automation and its prompt, which follow the
  procedure below from outside this repository.
- The gate or broker landing path that performs an authorized merge into
  `main`.
- The fork correspondence map in `docs/fork/`, which supplies bundle
  attribution as a fixed historical input.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-RELEASE-FORK-OVERLAY-PATCH-SET-001` | [Cutting the patch set](#cutting-the-patch-set), [Reproducing the overlay](#reproducing-the-overlay) |
| `REQ-RELEASE-FORK-OVERLAY-PATCH-SET-002` | [Data and contracts](#data-and-contracts) |
| `REQ-RELEASE-FORK-OVERLAY-PATCH-SET-003` | [Removing verified duplicates](#removing-verified-duplicates) |
| `REQ-RELEASE-FORK-OVERLAY-PATCH-SET-004` | [Adopting the overlay in main](#adopting-the-overlay-in-main) |
| `REQ-RELEASE-FORK-OVERLAY-PATCH-SET-005` | [Sync procedure](#sync-procedure), [Failure and recovery](#failure-and-recovery) |

## Components and responsibilities

| Component | Responsibility |
| --- | --- |
| `patches/NN-<slug>.patch` | One mailbox-format patch per bundle, created by `git format-patch --binary`. Each patch owns a disjoint set of fork-delta files. |
| `patches/INDEX.md` | Records the upstream base, the observed upstream `main`, the source revision, the application order, bundle metadata, and the total line count. It is never passed to `git am`. |
| `kd/overlay` | A disposable branch rebuilt at the upstream base by applying the patches. It is regenerated, never edited directly. |
| Fork `main` | Keeps all existing history and adopts an overlay result only through an authorized merge commit. |

## Data and contracts

### Upstream base

A cut uses `git merge-base <source> upstream/main`: the newest upstream commit
already merged into the source revision. Cutting at that base makes the
overlay tree equal the source tree, which proves the cut lost nothing.

The overlay contract is relative to the approved fixed upstream snapshot that
fork `main` has merged; for migration stage M1 that is `51af93dcd86b`, merged
by #296. A cut at that merge base is the completion check. A newer upstream
`main` is adopted by a later approved sync, which merges it into fork `main`
first and then cuts again; conflicting bundles are ported by hand there.

### Bundle attribution

The named bundles come from the `patch_bundle` column of
`docs/fork/commit-map.csv`:

| Order | Patch | Bundle |
| --- | --- | --- |
| 20 | `20-dynamic-tier.patch` | D02 dynamic profile tier selection |
| 21 | `21-provider-limits.patch` | D03 per-model provider limit suspension |
| 22 | `22-control-capacity.patch` | D06 control-session capacity |
| 23 | `23-peer-batch.patch` | D09 peer report batch delivery |
| 30 | `30-office-overview.patch` | D01 Office overview |
| 90 | `90-fork-remainder.patch` | Every other fork-delta file |

A fork-delta file joins a named bundle only when every fork non-merge commit
that touched it belongs to that bundle. Commits classified as `sync` are
ignored, because they carry upstream content. A file touched by two bundles,
by an unattributed commit, or only by a merge commit stays in the remainder.
This rule keeps each named patch free of unrelated changes. Shared files move
out of the remainder only after a later classification attributes them.

Attribution is by whole file. Hunk-level splitting is excluded: it would make
patches depend on each other and break independent removal.

### Index fields

Each bundle row records order, patch file, file count, changed lines (added
plus removed), upstream relation, state, and removal condition. States are
`open`, `merged`, `replaced`, `plugin`, and `dropped`. The total changed-line
count is the fork-size metric reported on every sync. Squashing or deleting
behavior does not count as a reduction; work moved to a plugin or CM is
reported separately.

## Control flow

### Cutting the patch set

1. Run `git fetch upstream` and `git fetch origin`. Record `upstream/main` and
   the source revision.
2. Compute the upstream base and the fork delta with
   `git diff --no-renames --name-status <base> <source>`, excluding
   `patches/`.
3. Assign each delta file to a bundle with the attribution rule.
4. Build one commit per bundle in order with a temporary index file, so the
   working tree is not touched:
   - `git read-tree <base>` into the temporary index.
   - Stage the bundle's files at their source blob, or remove deleted files,
     with `git update-index --index-info`.
   - Create the commit with `git write-tree` and `git commit-tree`.
5. Run `git format-patch --binary --no-numbered <base>..<last>` and rename
   each output to its `NN-<slug>.patch` name.
6. Write `INDEX.md` and commit `patches/` on the source branch.

### Reproducing the overlay

```sh
git switch -c kd/overlay <base>
{ git cat-file blob <patch-commit>:patches/20-dynamic-tier.patch
  git cat-file blob <patch-commit>:patches/21-provider-limits.patch
  git cat-file blob <patch-commit>:patches/22-control-capacity.patch
  git cat-file blob <patch-commit>:patches/23-peer-batch.patch
  git cat-file blob <patch-commit>:patches/30-office-overview.patch
  git cat-file blob <patch-commit>:patches/90-fork-remainder.patch
} | git am --keep-cr --3way
git diff --quiet <source> kd/overlay -- . ':(exclude)patches'
```

The base does not contain `patches/`, so the patches are read from the commit
that holds them and streamed to `git am` as one mailbox; no copy outside the
repository is needed. `--keep-cr` preserves carriage returns in files stored
with CRLF line endings. `--3way` lets a patch apply when its context moved
upstream. The patch list is the `INDEX.md` order, written out explicitly;
never rely on a shell glob. The tree comparison applies when `<base>` is the
cut's merge base.

### Removing verified duplicates

A removal candidate is checked against the pinned upstream base:

- The upstream source must provide the same behavior or a check that covers
  the fork check's assertions.
- A fork test that still guards a fork-only production change is retained.
- PR titles, merge state, or inventory counts are never sufficient.

Removals land in the source revision before the cut, so no patch carries a
removed duplicate. Unverified candidates are listed in the plan with the
reason they remain.

### Adopting the overlay in main

The overlay starts at an upstream commit, so it is not a descendant of `main`.
Adoption creates a merge commit whose first parent is the current `main` and
whose second parent is `kd/overlay`. Its tree is the overlay tree plus the
current `patches/` directory. When the overlay was cut from the current
`main`, that tree equals the `main` tree, which is the `-s ours` form.

The merge commit lands only through the authorized gate or broker landing
path with a merge-commit method. A squash merge, fast-forward, or tree-only
replacement is not used. After landing,
`git merge-base --is-ancestor <previous main> main` must be true.

### Sync procedure

The CM-owned automation follows this order (RB-3):

1. Fetch upstream and origin. Record the new upstream `main` as the candidate
   base and the current fork `main`.
2. If fork `main` differs from the index source revision outside `patches/`,
   cut the patch set again from the current `main` first. Applying an older
   cut would drop every fork change made after it.
3. Recreate `kd/overlay` at the candidate base and apply the patches in
   `INDEX.md` order with the command above.
4. Run `scripts/verify-release.ps1` against the overlay revision.
5. On success, confirm that fork `main` has not moved since step 1; if it has,
   hold the adoption and start again from step 1. Otherwise adopt the overlay
   in `main` through the merge path above, with the overlay tree plus
   `patches/` as the merge tree. Then build the overlay revision verified in
   step 4. The adopted `main` has the same tree outside `patches/` but a
   different commit, so building the overlay revision keeps `system/info`
   equal to the verified revision.
6. Update `INDEX.md`: mark bundles confirmed merged upstream or moved to a
   plugin, delete their patches, and report the total line count.

## Failure and recovery

- **Patch fails to apply:** run `git am --abort`. Do not use `git am --skip`.
  Mark only that bundle as held in the run log, and open one follow-up task to
  port it by hand. An overlay with a held bundle is not adopted, because it
  would drop retained behavior.
- **Tree mismatch after reproduction:** the cut is wrong. Regenerate the patch
  set from the recorded source; do not edit the overlay branch.
- **Verification fails:** keep `main` unchanged. The rollback is keeping
  `main`, which no step rewrites.
- **Source moves after the cut:** the index names its source revision. The
  next sync cuts again from the current `main` before applying (sync step 2),
  and holds adoption if `main` moves during the run (sync step 5).

## Persistence

Patches and the index are committed files on fork `main`. `kd/overlay` is a
regenerated branch. Fork `main` history is append-only: no force push, reset,
deletion, or rename.

## Security

Patches contain only repository content. The upstream remote stays fetch-only.
No credentials, operating database, or installed home are read or changed.

## Observability

The sync run log records the upstream base, the observed upstream `main`, the
applied and held bundles, the index total, the verification outcome, and the
adopted revision. After an installed build, `GET /api/v1/system/info` reports
the revision, which must match the verified overlay revision.

## Related decisions

- [Fork commit correspondence](../../../fork/README.md)
- [Restart resume turn start design](../../agents/system-design/resume-turn-start.md)
