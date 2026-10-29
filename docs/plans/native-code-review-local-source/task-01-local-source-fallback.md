---
id: "01-local-source-fallback"
title: "Task-bound local Git source fallback"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001
acceptance_criteria:
  - AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.1
  - AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.2
  - AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.3
system_design:
  - ../../specs/agents/system-design/native-code-review-recovery.md
---

# Task 01: Task-bound local Git source fallback

## Summary

When the agentctl read a review normally uses fails for a local or worktree
executor, read the task's own persisted checkout directly so the review can
still collect the working tree, staged, untracked, and committed changes.

## In scope

- `internal/review/local_git.go` and `internal/review/local_source.go`: the
  bounded local Git reader (`Source`, `ValidateSource`, `CollectLocalChanges`).
- `internal/backendapp/review_local_source.go`: the task-bound session reader
  that admits only `local`, `worktree`, and `local_pc` executors.
- `internal/agent/handlers/review_change_source.go`: the fallback branch in
  `ReviewChangeSource.UncommittedFiles` / `CommittedFiles`.

## Out of scope

The caller-supplied filesystem path API (an explicit operator/admin path), any
grant framework, any schema change, remote executors, and the runtime
review/utility integration.

## Verification

- `go test -trimpath ./internal/review/... -run 'CollectLocalChanges' -count=1`
- `go test -trimpath ./internal/backendapp/... -run 'ReviewAgentctlUnavailableUsesLocalGit' -count=1`
- The cases live in the existing `internal/review/diff_test.go` and
  `internal/backendapp/review_routing_test.go` files.
