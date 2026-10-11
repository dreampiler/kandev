---
created: 2026-10-11
status: done
requirements:
  - REQ-TASKS-ADDITIONAL-SESSION-WORKSPACE-REUSE-001
system_design:
  - ../../specs/tasks/system-design/additional-session-workspace-reuse.md
legacy_specs: []
---

# Implementation Plan: Retry a session binding after the workspace election

## Overview

Two starts for the same task's first session, or for members of one shared
workspace group, race the workspace materializer election. The loser's binding
is refused with `ErrWorkspacePreparing` while the winner creates the
environment, and session preparation returns that refusal at once. An
automation run, or any start that does not retry by itself, then fails even
though the workspace becomes ready moments later. The launch path already waits
for a creating sibling; session preparation does not.

## Scope

### In scope

- Carry the elected environment ID on the preparing refusal for task-local and
  shared-group elections.
- Wait for that environment during session preparation and retry the binding
  once.
- Update the workspace state matrix and the launch admission design.

### Out of scope

- Changing the election itself or the materialization timeout.
- Retrying refusals for other reasons.

## Technical approach

`models.WorkspacePreparingError` unwraps to `ErrWorkspacePreparing` and names the
environment being materialized. The repository returns it from the task-local
and shared-group binders with the existing messages. `prepareSessionAttempt`
calls `retryPreparedSessionAfterWorkspaceReady` for a preparing refusal: it
loads the named environment, skips one this session materializes, waits with
the existing `waitForTaskEnvironmentReady`, and retries the binding once.
Before the retry it re-reads the task's sessions, so a session the winner
created meanwhile keeps the first-session origin and the primary flag. A
failed environment or a wait that ends without readiness returns the original
refusal.

## Tests

`executor_workspace_preparing_retry_test.go` covers the retry, including a
shared-group member that binds through the group binder, a winner session that
must stay primary, the failed and timed-out waits, a self-owned environment, and
refusals that are not retried.
`task_environment_test.go` checks that the task-local and shared-group refusals
name the elected environment. AC-TASKS-ADDITIONAL-SESSION-WORKSPACE-REUSE-001.4.

## E2E tests

Executor and repository tests cover the change. No rendered UI changes require
browser coverage.

## Work orders

- [x] [Task 01: Retry after the workspace election](task-01-retry-after-workspace-election.md)

## Verification results

The retry and environment-ID checks failed without the change and pass with it.

## Risks

A start that loses the election now waits up to the launch timeout before it
returns, as the launch path already does for a creating sibling.
