---
created: 2026-10-13
status: in-progress
requirements:
  - REQ-AGENTS-RETAINED-CHECKOUT-001
system_design:
  - ../../specs/agents/system-design/retained-checkout-preparation-reconciliation.md
legacy_specs: []
---

# Implementation plan: Retained checkout preparation reconciliation

## Overview

Fresh task worktree materialization must reconcile a checkout retained at the
assigned path from an earlier attempt of the same task, instead of failing
`git worktree add` with `already exists`. Eligible occupants (healthy, clean
including ignored entries, fully reflected) are retired through the existing
managed no-follow removal; every other occupant is preserved and reported.

## Scope

In scope: occupant detection and classification on the fresh task-branch path,
single-reservation lock ownership, read-only ownership-marker validation before
any create/write, an active/live-writer guard, and retirement of eligible
occupants. Out of scope: recorded-recreate, attach-only reuse, existing-branch
checkout, contribution, and recovery/rematerialization paths; any new registry,
hash/verifier, or global scan.

## Technical approach

`internal/worktree` owns the reconcile helper (`manager_retained_checkout.go`)
and the fresh-only entry `addFreshWorktreeForBranch` (called from
`createInTaskDir`). It reuses `validateExistingWorktreePathOwner`,
`inspectLinkedWorktree`, `checkoutMatchesRepositoryIdentity`,
`runBoundedGitInspect`, `refContains`, `removeRecreatedWorktreePath`, and
`acquireWorktreeTargetPath`. `prepareTaskWorktreePath` gains a read-only
`ensureTaskDirNotOwnedByAnotherTask` check before it creates or marks the task
directory.

## Results

Implemented and locally verified: nine focused `RetainedCheckout` Go units pass
(eligible old branch, dirty, ignored content, unreflected, verified
non-ancestor squash, absent baseline, active row, foreign-task marker, foreign
repository), `go build ./...`, `go vet ./internal/worktree/`, and `gofmt` are
clean. Fork CI/review and integration remain pending.

## Risks

- Retirement must never discard unique work; eligibility is strict and fails
  closed on any read error.
- The retired path must be re-created under the same reservation so no
  concurrent preparer interleaves.

## References

- [Requirements](../../specs/agents/requirements/retained-checkout-preparation-reconciliation.md)
- [System design](../../specs/agents/system-design/retained-checkout-preparation-reconciliation.md)
- [Decision](../../decisions/2026-09-01-preserve-checkouts-during-inventory-repair.md)
- [x] [Task 01: Reconcile retained checkout](task-01-reconcile-retained-checkout.md)
