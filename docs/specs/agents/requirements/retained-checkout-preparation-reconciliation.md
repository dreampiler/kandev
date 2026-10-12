---
status: active
system: agents
created: 2026-10-13
owners:
  - kandev
---
# Retained checkout preparation reconciliation Requirements

## Overview

A fresh task-environment preparation can find the assigned per-task worktree
path occupied by a checkout retained from an earlier attempt of the SAME task.
The durable reuse lookup is keyed by `(session, repository, branch-slug)` and the
recorded worktree id, so a new session/environment with no matching row falls
through to `git worktree add` at the occupied path, which git rejects with
`already exists`. Preparation must recognize that retained checkout and follow
the existing reuse/retirement contract: retire it when its work is fully
reflected and its working tree is clean, and preserve and report it otherwise.

## Requirements

### REQ-AGENTS-RETAINED-CHECKOUT-001: Retained checkout preparation reconciliation

**Intent:** Fresh worktree materialization must reconcile a retained same-task
checkout occupying the assigned path instead of failing `git worktree add` with
`already exists`, and must preserve anything it cannot prove safe to retire.

#### Acceptance criteria

- **AC-AGENTS-RETAINED-CHECKOUT-001.1:** When the assigned path is absent, preparation behaves exactly as today.
- **AC-AGENTS-RETAINED-CHECKOUT-001.2:** On the fresh task-branch materialization path (the plain task branch), when the assigned path is occupied, preparation classifies the occupant before invoking `git worktree add`; it never relies on parsing git's `already exists` failure as the only signal. Existing-branch checkout, PR-checkout, recorded-recreate, attach-only reuse, contribution, and recovery/rematerialization sub-paths keep their existing behavior.
- **AC-AGENTS-RETAINED-CHECKOUT-001.3:** Preparation retires an occupant through the existing managed no-follow removal plus `git worktree prune` only when, in order: (a) the task-root ownership marker identifies the requesting task; (b) no live writer claims the path (no active `task_environment_repos` row for the task resolves to the assigned path, via `GetWorktreesByTaskID`); (c) the occupant is a structurally healthy linked worktree; (d) its Git common directory is the same file as the requested repository path; (e) its working tree is clean including ignored entries; (f) its committed history is fully reflected in the integration/base ref. Any ownership/active read error preserves and fails closed.
- **AC-AGENTS-RETAINED-CHECKOUT-001.4:** This fresh path never adopts, re-keys, or reuses an occupant; ownership/admission is never inferred from a matching branch slug or a merely present row. Legitimate same-owner reuse/attach remains under the existing record-based `tryReuseExisting`/`ReuseRequired` contract.
- **AC-AGENTS-RETAINED-CHECKOUT-001.5:** Preparation preserves and returns a stable typed non-leaking error, without deletion, reset, reseed, or rematerialization, when the occupant is dirty (including ignored entries), unreflected, a different repository, a main checkout, another task's root, still claimed by an active row or recovery claim, or when any ownership/active/identity read errors.
- **AC-AGENTS-RETAINED-CHECKOUT-001.6:** The existing `acquireWorktreeTargetPath` reservation is acquired exactly once for the occupant-check-and-add sequence and never re-acquired; the occupant check runs inside that single held region.
- **AC-AGENTS-RETAINED-CHECKOUT-001.7:** Retirement is a single managed removal. Pre-removal validation failure leaves the occupant untouched; a removal failure fails closed and re-inspects/reports the path's actual state, with byte-preservation guaranteed only up to the removal boundary.
- **AC-AGENTS-RETAINED-CHECKOUT-001.8:** The behavior applies only to the fresh materialization path; attach-only reuse (`ReuseRequired`) and the recorded-recreate path do not delete or retire any occupant.
- **AC-AGENTS-RETAINED-CHECKOUT-001.9:** The change adds no registry, no hash/fingerprint verifier, no global filesystem scan, and no new metric/validator/E2E obligation.

## Migrated source detail

This requirement is adjacent to, but distinct from,
[Agent Resume and Runtime Recovery](agent-resume-runtime-recovery.md)'s
preserved-workspace inventory repair (REQ-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-008),
which repairs durable inventory metadata for recorded slots without touching the
checkout. This requirement covers an unrecorded occupant at the assigned path
during fresh materialization.
