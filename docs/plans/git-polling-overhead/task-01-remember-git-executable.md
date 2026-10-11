---
id: "01-remember-git-executable"
title: "Remember the resolved Git executable"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-GIT-SUBPROCESS-ADMISSION-002
acceptance_criteria:
  - AC-PLATFORM-GIT-SUBPROCESS-ADMISSION-002.7
  - AC-PLATFORM-GIT-SUBPROCESS-ADMISSION-002.1
system_design:
  - ../../specs/platform/system-design/git-subprocess-execution.md
---

# Task 01: Remember the resolved Git executable

## Summary

Build managed Git commands from a remembered absolute Git path while the
process `PATH` is unchanged and the file still exists, instead of walking
`PATH` for every command.

## In scope

1. Add source-adjacent tests in `internal/common/subproc` before the change:
   - Under an unchanged `PATH`, a second command reuses the first resolved
     executable even after an earlier `PATH` directory gains a `git`
     candidate. This is the observable form of "no new `PATH` search" and
     fails on the baseline, which picks the new candidate.
   - A changed `PATH` resolves again.
   - Removing the remembered file resolves again.
   - `cmd.Args[0]` stays `git`; an unresolvable Git keeps the `git` name and
     the existing not-found error.
   Fixtures are empty placeholder files (`git.exe` on Windows, mode `0755`
   elsewhere); they are resolved, never executed. Each test resets the memo.
2. Add a mutex-guarded memo of the resolved path and the `PATH` value it was
   resolved under. `GitExecutablePath` checks the remembered file with one
   `os.Stat`, resolves again on a `PATH` change or missing file, and clears the
   memo when resolution fails.
3. `NewGitCommand` builds the command from the remembered path when available
   and falls back to `git`; `cmd.Args` stays `git` followed by caller arguments.
   Keep the comment that callers validate refs, paths, and options.

## Out of scope

Environment preparation, admission, lifecycle helpers, the raw Git audit, and
any other executable lookup.

## Acceptance

- The reuse test fails on the baseline and passes after the change; the other
  new tests pass on both.
- Existing environment and audit tests still pass.

## Verification

```bash
(cd apps/backend && go test -trimpath ./internal/common/subproc -run '^Test(NewGitCommand|GitExecutablePath)' -count=1)
(cd apps/backend && go test -trimpath ./internal/common/subproc -run '^Test(GitFinalEnvironment|PrepareGitCommandPreservesNilAndDuplicateEnvironment|GitRunnersApplyFinalEnvironment|ProductionGitCommandsUseTheAdmissionSeam|ScanGoSource)' -count=1)
(cd apps/backend && go build ./... && go vet ./internal/common/subproc/...)
```

## Files likely touched

- `apps/backend/internal/common/subproc/git_command.go`
- `apps/backend/internal/common/subproc/git_command_test.go`

## Dependencies

None.

## Risks

Package-level state is shared by every test in the package; tests that change
`PATH` already resolve again because the key changes.

## Results

Pending implementation.
