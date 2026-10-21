---
id: "01-resume-through-process-handle"
title: "Resume suspended children through the process handle"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-WINDOWS-SUSPENDED-RESUME-001
acceptance_criteria:
  - AC-PLATFORM-WINDOWS-SUSPENDED-RESUME-001.1
  - AC-PLATFORM-WINDOWS-SUSPENDED-RESUME-001.2
  - AC-PLATFORM-WINDOWS-SUSPENDED-RESUME-001.3
system_design:
  - ../../specs/platform/system-design/windows-suspended-child-resume.md
---

# Task 01: Resume suspended children through the process handle

## Summary

Replace the Toolhelp thread snapshot in `ResumeSuspendedProcess` with
`NtResumeProcess` on a `PROCESS_SUSPEND_RESUME` handle to the child.

## In scope

- `internal/common/winproc/lifecycle_windows.go`.
- In-process Windows tests in `internal/common/winproc`.

## Out of scope

- Process attributes and Job Object lifetimes of the callers.
- Starting processes through `windows.CreateProcess`.

## Acceptance

- `AC-PLATFORM-WINDOWS-SUSPENDED-RESUME-001.1` through
  `AC-PLATFORM-WINDOWS-SUSPENDED-RESUME-001.3` describe the behavior covered by
  this work order.

## Verification

```bash
cd apps/backend && go test -tags fts5 ./internal/common/winproc -run '^(TestResumeSuspendedProcess|TestNtResumeProcess)' -count=1
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

The existing Windows Job Object lifecycle tests start real suspended children.
They were not run for this change because they spawn processes.

## Results

- The in-process resume tests pass.
