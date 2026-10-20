---
id: "01-checkout-branch-slot-adoption"
title: "Adopt the canonical slot of a preserved checkout branch"
status: in-progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-008
acceptance_criteria:
  - AC-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-008.11
system_design:
  - ../../specs/agents/system-design/workspace-inventory-repair.md
---

# Task 01: Adopt the canonical slot of a preserved checkout branch

## Summary

Let a worktree resume or new session reuse its preserved checkout when the
task repository's `checkout_branch` equals the branch that checkout already
holds, while the canonical row stays keyed by the original slot identity.

## In scope

- Bind the launch's branch identity slug to the canonical row whose
  `worktree_branch` equals the requested checkout branch, only without an
  exact slot match.
- Require one active holder in the same repository whose adopted identity
  matches that row alone, and refuse rows a sibling requested slot matches.

## Out of scope

- Guarded repair, the user-owned local repository refusal, unarchive branch
  restoration, and error copy.

## Acceptance

- AC-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-008.11

## Verification

- `go test ./internal/orchestrator/executor/ -run 'CheckoutBranch|ExactCheckoutSlot'`

## Results

The resume regression failed before the change with the field error
`canonical workspace repository inventory has no matching entry for repository
"repo-1" branch "feature-ver09-m7-slack-offic-6zc"` and passes after it; the
different-branch, ambiguous-holder, deleted-row, sibling-slot, and exact-match
guards pass. In the full executor package,
`TestBuildResumeRequestRetryAfterCommittedRepairSucceeds` and
`TestLaunchPreparedSessionAutoRepairsZeroInventoryFromPriorEnvironmentRuntime`
fail identically with and without this change on the Windows development host.
