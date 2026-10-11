---
id: "02-checkout-without-git-metadata"
title: "Treat a checkout without Git metadata as absent"
status: done
wave: 1
depends_on:
  - "01-recover-preparation"
plan: "plan.md"
requirements:
  - REQ-TASKS-RUNTIME-CLEANUP-001
acceptance_criteria:
  - AC-TASKS-RUNTIME-CLEANUP-001.9
system_design:
  - ../../specs/tasks/system-design/runtime-cleanup-preparation.md
---

# Task 02: Treat a checkout without Git metadata as absent

## Summary

`CaptureCleanupHeadOIDs` treated any existing directory as a live checkout and
ran `git rev-parse HEAD` in it. When the checkout's `.git` file is gone but the
directory survives inside another repository, Git discovers that enclosing
repository and its HEAD is recorded as the worktree's cleanup identity. Only
checkout-local Git metadata may supply HEAD; otherwise the exact branch
inspection from Task 01 applies.

## In scope

- Add a capture test for a checkout directory without `.git` inside another
  repository before the change.
- Require checkout-local `.git` before reading HEAD, both before inspection and
  when the inspection fails.
- Record the rule in the runtime cleanup preparation design.

## Out of scope

Branch inspection itself, cleanup audits, and other worktree states.

## Acceptance

- A checkout directory without `.git` records the worktree branch's commit, not
  the enclosing repository's HEAD.
- Existing capture tests keep their behavior.

## Verification

From the repository root:

```bash
(cd apps/backend && go test -trimpath ./internal/worktree -run 'TestCaptureCleanupHeadOIDs' -count=1)
(cd apps/backend && go vet -trimpath ./internal/worktree)
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

## Files likely touched

- apps/backend/internal/worktree/manager_cleanup.go
- apps/backend/internal/worktree/manager_cleanup_capture_test.go
- docs/specs/tasks/system-design/runtime-cleanup-preparation.md

## Dependencies

Task 01.

## Risks

Low: a checkout whose `.git` cannot be read for another reason now fails the
capture closed through the existing inspection-error classification.

## Parallelism

`sequential`

## Inputs

- REQ-TASKS-RUNTIME-CLEANUP-001 and AC-TASKS-RUNTIME-CLEANUP-001.9.
- The linked runtime cleanup preparation design.

## Results

- RED: before the change, `TestCaptureCleanupHeadOIDs_CheckoutWithoutGitMetadataUsesBranch`
  recorded the enclosing repository's HEAD.
- GREEN: the new test passes; `TestCaptureCleanupHeadOIDs_PathRemovedDuringInspection`
  uses a shell-script fake `git` that deletes its working directory and fails on
  Windows with and without this change.
