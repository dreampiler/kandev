---
id: "01-implementation"
title: "feat(automation): support optional parent tasks"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-OFFICE-AUTOMATION-TARGETS-004
acceptance_criteria:
  - AC-OFFICE-AUTOMATION-TARGETS-004.1
  - AC-OFFICE-AUTOMATION-TARGETS-004.2
  - AC-OFFICE-AUTOMATION-TARGETS-004.3
  - AC-OFFICE-AUTOMATION-TARGETS-004.4
  - AC-OFFICE-AUTOMATION-TARGETS-004.5
  - AC-OFFICE-AUTOMATION-TARGETS-004.6
system_design:
  - ../../specs/office/system-design/automation-target-modes.md
---

# feat(automation): support optional parent tasks

## Outcome and scope

Support an optional validated parent for both automation target modes, preserve retry snapshots, clear future continuation when the parent changes, and expose explicit relationships.

Out of scope: Changing ordinary board filtering, rewriting old tasks/history, and exporting instance-local parent IDs.

## Implementation acceptance

1. The affected native entrypoint follows the linked requirement and design while preserving unrelated behavior and history.
2. Rejected, stale and failed operations retain their existing refusal or rollback semantics.
3. The focused implementation checks and linked documentation coverage have honest recorded results; unexecuted follow-up checks remain pending.

## Files and dependencies

The source files are the existing PR #4723 diff. This work order adds its delivery record and links existing requirements and system design; it introduces no runtime gate or permanent test.

- [System design](../../specs/office/system-design/automation-target-modes.md)
- [Plan](plan.md)

## Verification and results

Validation: 25 existing frontend tests and changed-file ESLint passed. Focused Go checks plus disposable real SQLite journeys covered both task modes, persisted ParentID and relationship readback, invalid/foreign/nested parents, update/clear/unchanged continuation, retry snapshots and export omission. All normal commit hooks passed after a cache-warming retry of a zero-issue lint timeout. Temporary fixtures were removed; existing web fixtures were adapted without new standing test cases or CI.

Documentation-only correction: git diff --check and the existing .github/scripts/pr-docs.cjs validateCoverage entrypoint with the candidate changed paths and the linked candidate documents supplied on stdin. No GitHub status is synthesized locally.

## ASCII UI preview

UI-01: Existing affected surface, after this change.

```text
Automation editor
  Parent task (optional): [Search tasks]
  Target: hidden run | normal task
  [Save]

Clearing the picker removes future parent assignment. Desktop and phone keep the existing editor and save flow.
```

This sketch records the affected structure; spacing is illustrative. It does not add a new control or approval requirement.
