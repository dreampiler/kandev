---
status: draft
system: agents
created: 2026-10-29
owners:
  - jcfs
---

# Native Code Review Local Source Requirements

## Overview

A native [code review](native-code-review.md) reads a task's changed files from
the session workspace. This requirement covers reading that source directly from
the task's own checkout when the agentctl read is unavailable, without adding a
new source authority.

## Requirements

### REQ-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001: Task-bound local review source

**Intent:** A review for a task whose executor is local or worktree can still
collect the task's changes when agentctl is unavailable, without gaining access
to any path the task does not already own.

#### Acceptance criteria

- **AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.1:** When the agentctl read of a task's changed files fails and the task's executor is `local`, `worktree`, or `local_pc`, a review pass SHALL read the task's own persisted checkout to collect its changes instead of failing with `workspace_unavailable`. A remote executor SHALL still require agentctl.
- **AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.2:** The local read SHALL stay bounded to the existing per-file, per-output, and total diff byte limits; SHALL reject a worktree path that is not an absolute local path or that carries a `..` segment; and SHALL never accept a caller-supplied filesystem path.
- **AC-AGENTS-NATIVE-CODE-REVIEW-LOCAL-SOURCE-001.3:** The local read SHALL collect the working tree, staging-area (index) changes the working tree no longer shows, and untracked files, distinct from a committed base-to-head range, and SHALL keep multi-repository files scoped to their own repository.
