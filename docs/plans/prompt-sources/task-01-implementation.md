---
id: "01-implementation"
title: "feat(workflow): import revisioned prompt sources with native identity"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PROMPT-SOURCES-REVISION-001
acceptance_criteria:
  - AC-PROMPT-SOURCES-REVISION-001.1
  - AC-PROMPT-SOURCES-REVISION-001.2
  - AC-PROMPT-SOURCES-REVISION-001.3
  - AC-PROMPT-SOURCES-REVISION-001.4
  - AC-PROMPT-SOURCES-REVISION-001.5
  - AC-PROMPT-SOURCES-REVISION-001.6
system_design:
  - ../../specs/prompt-sources/system-design/revisioned-workflows.md
---

# feat(workflow): import revisioned prompt sources with native identity

## Outcome and scope

Import, preview and compare revisioned native workflow instances with source locking, preserved identity, atomic writes and post-commit events.

Out of scope: A second workflow engine, task/history replacement, new renderer syntax, and activating sources in an installation.

## Implementation acceptance

1. The affected native entrypoint follows the linked requirement and design while preserving unrelated behavior and history.
2. Rejected, stale and failed operations retain their existing refusal or rollback semantics.
3. The focused implementation checks and linked documentation coverage have honest recorded results; unexecuted follow-up checks remain pending.

## Files and dependencies

The source files are the existing PR #4714 diff. This work order adds its delivery record and links existing requirements and system design; it introduces no runtime gate or permanent test.

- [System design](../../specs/prompt-sources/system-design/revisioned-workflows.md)
- [Plan](plan.md)

## Verification and results

Validation:
- Existing focused workflow/task tests, backend authority wiring, SQL guard and SQLite workflow race conformance passed.
- Temporary actual HTTP/SQLite/bus checks passed for permissions, immutable revisions, rollback, occupied steps, stable identity/history, native prompt consumption and committed event delivery.
- Frontend lint, full TypeScript checking and all normal repository commit hooks passed.
- Live PostgreSQL was not exercised. No permanent tests, new event protocol, background process or operating import was added.

Documentation-only correction: git diff --check and the existing .github/scripts/pr-docs.cjs validateCoverage entrypoint with the candidate changed paths and the linked candidate documents supplied on stdin. No GitHub status is synthesized locally.

The original c100 source overlay reproduced all three reviewed defects (package 1.122s). The candidate passed the same HTTP/SQLite/runtime-entrypoint cases plus transaction recheck/rollback, no-op, event, timestamp, stable-ID/start/order-revision controls with -race (2.496s). Five selected existing GitHub-sync name/removal/no-op checks passed with -race (1.207s); SQL guard passed. Normal commit hooks must pass before publication. Live PostgreSQL was not run.

## ASCII UI preview

UI-01: Existing affected surface, after this change.

```text
Workflow card
  Source locked: on
  [Edit definition: disabled]

An explicitly unlocked instance keeps the existing editor. Desktop and phone retain their current card composition.
```

This sketch records the affected structure; spacing is illustrative. It does not add a new control or approval requirement.


## Portable stage classification correction

The existing native definition conversion must retain its persisted stage type.
The portable step now carries optional stage_type in the existing export version
envelope. Recognized work/review/approval/custom values survive export, preview
and import. New steps from older inputs default to custom; sync preserves a
matched existing type when an older source omits it, and applies and reports
explicit type changes as drift. Unknown supplied types are rejected.

This correction preserves profile-selection, source permissions, stable identity
and history. Office-style exclusion, participant assignments, decisions and
execution authority are unchanged. It does not add hold classification or
backfill. Existing public import/export and sync guides state that boundary.

Root verification reproduced all three original defects: exported stage type
was absent, unknown supplied types were ignored, and explicit sync types were
not applied. The final publication checkout passed the same disposable cases
and existing export/profile-selection/sync-no-op/stable-ID/persisted-stage
controls with the race detector: models1.113s, service1.911s, repository1.434s.
Missing-field compatibility and export-version selection were preserved.
No permanent test or installed workflow activation was added. Normal commit
hooks and updated-head hosted CI are separate publication results; historical
verification above is not substituted for the new checks.
