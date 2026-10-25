---
id: "01-implementation"
title: "fix(worktree): reuse retained checkouts at assigned task paths"
status: done
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

# fix(worktree): reuse retained checkouts at assigned task paths

## Outcome and scope

Reuse an exact retained assigned checkout before refresh/creation, preserving valid registration or attaching an eligible unregistered checkout through normal materialization.

Out of scope: Cleaning/resetting/moving checkouts, adopting conflicting slots, broad filesystem discovery, and new ownership proofs.

## Implementation acceptance

1. The affected native entrypoint follows the linked requirement and design while preserving unrelated behavior and history.
2. Rejected, stale and failed operations retain their existing refusal or rollback semantics.
3. The focused implementation checks and linked documentation coverage have honest recorded results; unexecuted follow-up checks remain pending.

## Files and dependencies

The source files are the existing PR #4722 diff. This work order adds its delivery record and links existing requirements and system design; it introduces no runtime gate or permanent test.

- [System design](../../specs/agents/system-design/retained-checkout-preparation-reconciliation.md)
- [Plan](plan.md)

## Verification and results

Validation: focused existing worktree/executor checks and disposable real-Git Create/reuse journeys passed, including registered and unregistered reuse, unchanged IDs, registration failure, foreign/main checkout preservation and conflicting-slot refusal. All normal commit hooks passed and temporary probes were removed. No standing tests, new hashes, CI or operating mutations were added.

Documentation-only correction: git diff --check and the existing .github/scripts/pr-docs.cjs validateCoverage entrypoint with the candidate changed paths and the linked candidate documents supplied on stdin. No GitHub status is synthesized locally.

## Current-main checkout identity integration

The retained-checkout path and upstream verified branch-evolution path keep their
distinct contracts. The canonical InspectCheckoutIdentity and its shared
inspectCheckoutIdentity helper remain unchanged from upstream, including branch
and reciprocal-registration validation. The retained path keeps its existing
branch-independent common-directory predicate before its separate task, slot,
checkout-branch and contribution checks. No main-checkout relocation or new
ownership/content proof is introduced.

The add/add file conflict is resolved by keeping both APIs without copying the
upstream preservation helper into a second location. Prior retained reuse and
contribution/environment checks remain valid for unchanged source. Narrow real
Git coexistence checks use a disposable overlay outside permanent collection.
The merge exposed a Windows raw-Git/native-path separator mismatch, corrected by
normalizing the registered path with the existing canonical helper. Branch,
reference, commit and unavailable-path refusals remain unchanged. Real retained
Create plus verified branch/stale/foreign controls passed with race detection
(worktree 4.464s); the prior main-checkout refusal pass is reused.

Existing upstream verified-branch resume controls then passed under race
detection (executor 29.847s), including switch/rename and stale-branch refusal.
Those fixture subprocesses used an empty temporary Git global configuration and
disabled system config; assertions and product code were unchanged. The actual
user profile has nonempty Git LFS clean/process entries, which full preservation
intentionally refuses. This confirms clean fixture behavior, not acceptance of
that ambient filter profile. No user/global config, filter protection, ownership
proof or permanent test was changed. Normal commit and publication remain with
the parent integration owner.

The retained-slot supplemental criterion uses
AC-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-008.13. Its wording is unchanged from
this PR's earlier proposed 008.11; current upstream already owns 008.11 and
008.12 for attested branch evolution. Only the colliding identifier is corrected;
both behavior requirements and the earlier commits remain preserved.
