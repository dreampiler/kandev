---
status: draft
system: release
created: 2026-10-15
owners:
  - kandev
---

# Fork Overlay Patch Set Requirements

## Overview

The fork `dreampiler/kandev` carries local changes on top of upstream
`kdlbs/kandev`. Today those changes live only as fork `main` history: a few
hundred commits interleaved with upstream sync merges. That history cannot be
re-applied to a newer upstream, and nothing shows how much of the fork remains
fork-only.

The overlay patch set expresses the whole fork delta as ordered
`patches/*.patch` files plus `patches/INDEX.md`. The branch `kd/overlay` is
rebuilt from an upstream base with `git am`, and fork `main` adopts the overlay
result without losing its existing history. This turns each later upstream
merge or plugin migration into deleting one patch.

The release system owns this contract because the overlay result is the source
revision that fork releases are built and verified from. The workspace
automation that runs the periodic sync belongs to the configuration-management
(CM) owner outside this repository; this document fixes only the repository
contract that the automation follows.

The owner goal, translated, is: convert the fork into an overlay by removing
upstream-replaced traces, splitting `patches/` starting with the large retained
bundles, writing `INDEX.md`, and moving the sync automation to the RB-3
procedure. The rollback rule, translated, is: keep `main`, with no branch
rename and no force push. The verbatim originals stay in the migration plan
(stage M1) and its Kandev task.

## Terminology

- **Upstream base:** The `kdlbs/kandev` `main` commit that a patch set applies
  to. It is the newest upstream commit already merged into the source
  revision.
- **Source revision:** The fork commit whose tree the patch set reproduces.
- **Fork delta:** The tree difference from the upstream base to the source
  revision, excluding the `patches/` directory itself.
- **Bundle:** A named group of fork changes with one destination, such as an
  upstream PR, an upstream extension plus plugin, or removal. Bundle identities
  (`D02`, `D03`, `D01`, `D06`, `D09`) come from the fork classification
  table.
- **Remainder bundle:** The bundle holding every fork-delta file that is not
  yet attributed to exactly one named bundle.
- **Overlay:** The branch `kd/overlay`, equal to the upstream base plus the
  patch set applied in `INDEX.md` order.
- **Verified duplicate:** A fork change whose behavior and checks are already
  present in upstream, confirmed by comparing the pinned upstream and fork
  sources rather than by PR titles or merge state.

## Requirements

### REQ-RELEASE-FORK-OVERLAY-PATCH-SET-001: Reproducible overlay

**Intent:** Anyone with the repository and the upstream remote can rebuild the
fork source from upstream plus the recorded patches, with no hidden manual
step.

**User story:** As a fork maintainer, I want `kd/overlay` to be rebuilt from
the upstream base and the ordered patches, so that a newer upstream can be
adopted by re-applying patches instead of re-merging history.

#### Acceptance criteria

- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.1:** When a fresh `kd/overlay` is
  created at the recorded upstream base and every patch is applied with
  `git am` in `INDEX.md` order, every patch shall apply without manual edits.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.2:** After all patches apply, the
  overlay tree shall equal the recorded source revision tree outside the
  `patches/` directory.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.3:** Each fork-delta file shall be
  changed by exactly one patch, so that patches do not depend on hunks from
  another patch.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-001.4:** For stage completion, the
  patches shall apply on the `upstream/main` commit observed at fetch. A
  reproduction at an older merge base shall be reported as an intermediate
  check, not as completion.

### REQ-RELEASE-FORK-OVERLAY-PATCH-SET-002: Patch index

**Intent:** The index is the single readable record of what the fork still
carries and why.

**User story:** As a fork maintainer, I want one index that names the base,
the source, the order, and each bundle's destination and removal condition,
so that I can report how much fork code remains and what removes it.

#### Acceptance criteria

- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-002.1:** The head of `patches/INDEX.md`
  shall record the upstream base commit, the upstream `main` commit observed at
  fetch time, and the source revision.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-002.2:** Each bundle row shall record the
  application order, patch file, file count, changed-line count, upstream
  relation, state (`open`, `merged`, `replaced`, `plugin`, or `dropped`), and
  removal condition.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-002.3:** The index shall report the total
  changed-line count of all patches, and `INDEX.md` itself shall not be an
  input to `git am`.

### REQ-RELEASE-FORK-OVERLAY-PATCH-SET-003: Evidence-based removal

**Intent:** Removing fork code because upstream "already has it" must not drop
retained fork behavior.

**User story:** As a fork maintainer, I want only verified duplicates removed,
so that counts from an earlier inventory do not delete behavior that upstream
does not provide.

#### Acceptance criteria

- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-003.1:** When a fork change is removed as
  upstream-replaced, the record shall name the upstream source that provides
  the same behavior or check at the pinned upstream base.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-003.2:** When a candidate still guards a
  fork-only production change, or equivalence is not established, the change
  shall be retained and recorded as an unverified candidate.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-003.3:** Inventory counts from earlier
  classifications shall be reported as inputs, not as current measurements.

### REQ-RELEASE-FORK-OVERLAY-PATCH-SET-004: History-preserving main adoption

**Intent:** Adopting the overlay never rewrites or loses existing fork
history.

**User story:** As a fork maintainer, I want `main` to adopt the overlay
result through a merge commit, so that every existing commit stays reachable
and rollback means keeping `main`.

#### Acceptance criteria

- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-004.1:** When `main` adopts an overlay,
  the change shall be a merge commit made through the authorized gate or broker
  landing path, and `git merge-base --is-ancestor <previous main> main` shall
  be true afterwards.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-004.2:** Adoption shall not use force
  push, `reset --hard`, history deletion, or a branch rename, and it shall not
  use a fast-forward or tree-only replacement that breaks ancestry.

### REQ-RELEASE-FORK-OVERLAY-PATCH-SET-005: Sync procedure contract

**Intent:** The periodic upstream sync follows one procedure that fails safely
per bundle.

**User story:** As the CM owner of the daily sync automation, I want a fixed
repository procedure, so that a failing patch holds only its bundle and every
run leaves a readable result.

#### Acceptance criteria

- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-005.1:** When a patch fails to apply,
  the sync shall not use `git am --skip`; it shall mark only that bundle as
  held and report it for one follow-up task.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-005.2:** A sync run shall record the new
  upstream base, the applied bundles, the index total line count, and its
  outcome in a log that can be quoted.
- **AC-RELEASE-FORK-OVERLAY-PATCH-SET-005.3:** When an installed build runs
  from an adopted overlay, `GET /api/v1/system/info` shall report the overlay
  revision that the sync verified.

## Out of scope

- Writing or replacing the CM-owned sync automation prompt or scripts.
- Operating deployment, installation swaps, and restarts.
- Upstream GitHub publication of any bundle.
- Splitting the remainder bundle into further named bundles beyond `D02`,
  `D03`, `D01`, `D06`, and `D09`.
- New recurring tests, gates, ledgers, or CI checks for the patch set.
