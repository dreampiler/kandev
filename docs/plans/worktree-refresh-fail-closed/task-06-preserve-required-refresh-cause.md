---
id: "06-preserve-required-refresh-cause"
title: "Preserve the required refresh verification cause"
status: done
wave: 6
depends_on: []
plan: "plan.md"
requirements:
  - REQ-WORKSPACES-WORKTREE-BASE-REFRESH-001
acceptance_criteria:
  - AC-WORKSPACES-WORKTREE-BASE-REFRESH-001.24
system_design:
  - ../../specs/workspaces/system-design/worktree-base-refresh.md
---

# Task 06: Preserve the Required Refresh Verification Cause

## Summary

When a required checkout-branch refresh falls back to fetching the branch as a
remote-tracking ref and still cannot verify the refreshed ref, report the
verification cause instead of a generic command failure.

## Context

`fetchBranchToLocalWithPolicy` in `internal/worktree/manager_lifecycle.go`
handles a fetch Git refused because the branch is already checked out. It
retries via `retryFetchAsRemoteTrackingRef`. That result already carries the
user-facing `Warning` and the diagnostic `WarningDetail`, and the best-effort
path returns both to the caller. The required path tested `result.Warning != ""`
and then returned `ErrGitCommandFailed` without the cause, so an operator could
not distinguish a ref-verification refusal from a real command failure.

## In scope

- Resolve the diagnostic detail as `WarningDetail`, falling back to `Warning`
  when the detail is empty.
- Emit one structured warning with the branch and the resolved detail.
- Carry the resolved detail in the returned error before `ErrGitCommandFailed`.

## Out of scope

- The suppression of a result whose warning is empty.
- The non-required result path and every fallback decision.
- New diagnostic categories, logging configuration, retries, or persistence.
- The public-facing warning text returned by the best-effort path.

## Acceptance

- A required checkout-branch refresh that cannot verify the refreshed ref
  returns an error that contains the resolved verification cause.
- A structured warning records the branch and the resolved detail; the preserved
  cause contains no credentials, remote URLs, or raw command output. The
  resolved detail is the fixed probe string from `resolveRetryStartPoint`, not
  captured Git command output.
- A verified refreshed ref, an empty warning, the non-required result, and every
  fallback decision keep their current behavior.

## Verification

The change is a diagnostic-preservation fix in one function with no signature
change. Narrow checks:

```bash
# From apps/backend:
gofmt -l internal/worktree/manager_lifecycle.go
go build -trimpath ./internal/worktree/
go vet -trimpath ./internal/worktree/
go test -trimpath ./internal/worktree/ -run 'Fetch|Refresh|Retry'
```

The `go test` selection exercises the surrounding fetch/refresh paths; in the
Windows preparation worktree it fails with `fatal: not a git repository`, and
the same failure occurs on the untouched base, so it is an environment-limited
run rather than a change-caused result.

## Files likely touched

- `apps/backend/internal/worktree/manager_lifecycle.go`

## Dependencies

None.

## Risks

- Error wrapping can leak raw Git output. The detail is the bounded fetch
  output already produced by the refresh retry; do not append additional raw
  command output.

## Parallelism

`independent`

## Inputs

- `REQ-WORKSPACES-WORKTREE-BASE-REFRESH-001`.
- `docs/specs/workspaces/system-design/worktree-base-refresh.md`.
- `docs/specs/workspaces/requirements/worktree-base-refresh.md`
  (`AC-WORKSPACES-WORKTREE-BASE-REFRESH-001.14`, `.24`).

## Results

- Implemented in `manager_lifecycle.go`: the required path now resolves
  `WarningDetail` with a `Warning` fallback, logs branch/detail at warn, and
  includes the detail in the returned error before `%w`. The warning records no
  raw command output, matching `AC-WORKSPACES-WORKTREE-BASE-REFRESH-001.14`.
- `gofmt -l`, `go build`, and `go vet` on the package passed. The `go test`
  selection is an environment-limited run (see Verification).
