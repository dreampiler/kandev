---
created: 2026-10-29
status: done
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001
system_design:
  - ../../specs/agents/system-design/native-code-review-recovery.md
legacy_specs: []
---

# Implementation Plan: Native code review local source fallback

## Overview

A native code review reads a session's changed files through agentctl. When that
runtime is unavailable (stopped, restarted, or a local executor without a live
agentctl), the review fails with `workspace_unavailable` even though the task's
own checkout is still on disk on the backend host. This plan adds a bounded
local Git reader and a task-bound fallback so a review can still run for a local
or worktree executor.

## Scope

In scope: `internal/review/local_git.go`, `internal/review/local_source.go`, the
reader in `internal/backendapp/review_local_source.go`, the fallback branch in
`internal/agent/handlers/review_change_source.go`, and their cases in the
existing `internal/review/diff_test.go` and
`internal/backendapp/review_routing_test.go`.

Excluded: the caller-supplied filesystem path API (an explicit operator/admin
path), grant framework changes, schema changes, and the runtime review/utility
integration owned by its own design.

## Technical approach

`CollectLocalChanges` reads a local worktree's working tree (including staged
changes the working tree reverted, and untracked files) or a committed
`base..head` range, bounded by the same per-file, per-output, and total byte
limits agentctl uses, and rejects a worktree path with a `..` segment, a UNC
path, or a non-absolute path. The backend session reader resolves the session's
own persisted `Worktrees`/`WorkspacePath` and admits only `local`, `worktree`,
and `local_pc` executors. `ReviewChangeSource` falls back to the local reader
only when the agentctl read errors, so the success path and every other review
path are unchanged.

## Work orders

- [x] [Task 01: Task-bound local Git source fallback](task-01-local-source-fallback.md)

## Changed-path coverage

The work order owns the review collector, the backend session reader, and the
`ReviewChangeSource` fallback, their cases in the existing package test files,
the new criterion in the native code review requirement, and the matching note
in the review recovery system design.
