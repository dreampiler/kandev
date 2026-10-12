---
id: "01-reconcile-retained-checkout"
title: "Reconcile a retained checkout at the fresh task worktree path"
status: in-progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-RETAINED-CHECKOUT-001
acceptance_criteria:
  - AC-AGENTS-RETAINED-CHECKOUT-001.1
  - AC-AGENTS-RETAINED-CHECKOUT-001.2
  - AC-AGENTS-RETAINED-CHECKOUT-001.3
  - AC-AGENTS-RETAINED-CHECKOUT-001.4
  - AC-AGENTS-RETAINED-CHECKOUT-001.5
  - AC-AGENTS-RETAINED-CHECKOUT-001.6
  - AC-AGENTS-RETAINED-CHECKOUT-001.7
  - AC-AGENTS-RETAINED-CHECKOUT-001.8
  - AC-AGENTS-RETAINED-CHECKOUT-001.9
system_design:
  - ../../specs/agents/system-design/retained-checkout-preparation-reconciliation.md
---

# Task 01: Reconcile retained checkout

## Summary

Recognize a retained same-task checkout at the assigned worktree path during
fresh materialization, retire it when it is eligible, and preserve and report it
otherwise so preparation stops failing `git worktree add` with `already exists`.

## In scope

- Fresh-only reconcile entry (`addFreshWorktreeForBranch`) holding the existing
  target-path reservation exactly once.
- Read-only task-root ownership-marker validation before directory creation or
  marker write; live-writer guard from active durable rows and held recovery
  claims.
- Structural, repository-identity, cleanliness (including ignored entries), and
  reflection (ancestry or verified non-ancestor equivalence) checks.
- Retirement via the existing managed no-follow removal plus `git worktree prune`.
- Companion Go units alongside the changed source.

## Out of scope

- Recorded-recreate, attach-only reuse, existing-branch checkout, contribution,
  and recovery/rematerialization paths.
- Any new registry, hash/verifier, global scan, E2E/CI/hook, or permanent
  recurring validator.

## Acceptance

- Eligible retained same-task checkout is retired; preparation succeeds.
- Dirty, ignored-content, unreflected, foreign-repository, foreign-root, and
  active-owned occupants are preserved with a typed error.
- Verified non-ancestor (patch-equivalent) integration is eligible; unknown or
  error comparisons are not.
- No nested lock acquisition; exactly one reservation covers inspect → retire →
  add.

## Verification

```bash
cd apps/backend && go test ./internal/worktree/ -run 'RetainedCheckout' -count=1
cd apps/backend && go build ./...
cd apps/backend && go vet ./internal/worktree/
```

## Results

Nine focused units pass locally:
`TestRetainedCheckoutEligibleOldBranchIsRetired`,
`TestRetainedCheckoutDirtyIsPreserved`,
`TestRetainedCheckoutIgnoredContentIsPreserved`,
`TestRetainedCheckoutUnreflectedIsPreserved`,
`TestRetainedCheckoutVerifiedSquashIsRetired`,
`TestRetainedCheckoutAbsentCreatesFresh`,
`TestRetainedCheckoutActiveRowIsPreserved`,
`TestRetainedCheckoutForeignTaskMarkerIsPreserved`,
`TestRetainedCheckoutForeignRepositoryIsPreserved`.
`go build ./...`, `go vet ./internal/worktree/`, and `gofmt -l` are clean.
Fork CI, review, and integration remain pending.
