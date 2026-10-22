---
status: current
system: agents
requirements:
  - REQ-AGENTS-RETAINED-CHECKOUT-001
---

# Retained checkout preparation reconciliation system design

## Purpose and boundaries

This design lets fresh task-environment preparation reconcile a checkout
already present at the assigned per-task worktree path. The worktree manager
owns detection and the retirement decision; the existing managed no-follow
removal owns deletion; the existing task-root ownership marker and the durable
`task_environment_repos` rows own identity. No new registry, verifier, or
reconciliation framework is introduced.

It runs only on the fresh materialization path of `worktree.Manager.Create`
(`createInTaskDir` → the plain task-branch add). It never runs for the
recorded-recreate path (`gitAddWorktreeForRecreateLocked`), the attach-only
reuse path (`ReuseRequired`), or the existing-branch checkout path; those keep
their existing admission and removal contracts unchanged.

## Call path and lock ownership

`addFreshWorktreeForBranch` is the single fresh-only entry. For a plain task
branch it acquires the existing `acquireWorktreeTargetPath` reservation exactly
once and holds it across occupant reconciliation, eligible retirement, and the
git add (delegating to the existing `gitAddWorktreeLocked`), so there is no
nested acquisition of the non-reentrant lock and no inspect/removal/add race.
Requests that check out an existing branch keep the unchanged
`addWorktreeForBranch` dispatch.

## Detection and decision order

Order matters: ownership and live-writer guards run before any retirement
decision, and no occupant is ever adopted.

1. `os.Lstat(assignedPath)`: absent → proceed; any other error → preserve and report.
2. Task-root ownership marker (read-only, before any create/write): `validateExistingWorktreePathOwner` rejects a root owned by another task with `ErrWorktreePathOwnedByAnotherTask`. `prepareTaskWorktreePath` additionally performs a read-only `ReadOwnershipMarker` check (`ensureTaskDirNotOwnedByAnotherTask`) before it creates the task directory or writes the marker, so an occupied foreign root is never rebranded; the marker write itself is guard-validated (`existingMarkerMatches`).
3. Live-writer guard: any active `task_environment_repos` row for the task whose `worktree_path` equals the assigned path (`GetWorktreesByTaskID`) preserves and reports; a read error preserves and fails closed. Recovery claims for an already-existing environment are owned by the existing recovery-admission machinery, outside this fresh path (a fresh launch may have no bound environment yet).
4. Structural identity: `inspectLinkedWorktree` must classify `healthy`.
5. Repository identity: `checkoutMatchesRepositoryIdentity` must match the requested repository.
6. Cleanliness including ignored entries: `git status --porcelain=v1 --untracked-files=all --ignored=traditional` must be empty. Any ignored entry makes the occupant ineligible, so non-recoverable ignored content is preserved; only content the existing cleanup contract explicitly treats as recoverable is not unique work.
7. Reflection: `refContains` (ancestry, the existing `verifyHeadAgainstIntegration` proof) OR verified non-ancestor integration where every commit reachable from the occupant HEAD but not from the integration ref is content-equivalent in the base (`git cherry` yields no `+` line, requiring at least one compared commit). An unresolvable ref or an unknown/error comparison is not reflected. `integrationRef` resolves from `IntegrationRef`, then `BaseBranch`, then the fallback base.
8. Eligible → retire via `removeRecreatedWorktreePath` then `git worktree prune`. Otherwise → `WorktreeRecoveryError{State: "retained_checkout"}` preserving the occupant.

## Preserved invariants

- One canonical checkout per repository slot; a live checkout is never shared across branches.
- `git worktree add` is only reached after ownership, live-writer, and eligibility proofs; a present occupant is never discovered only through git's `already exists` failure.
- Anything that cannot be proven fully reflected and clean (including ignored entries) is preserved and surfaced, matching the preserve-checkouts decision.
- The existing same-owner attach/reuse behavior is untouched.

## Related decisions

- [Preserve checkouts during inventory repair](../../../decisions/2026-09-01-preserve-checkouts-during-inventory-repair.md)
