---
created: 2026-10-12
status: planned
requirements:
  - REQ-TASKS-RESUME-PROMPT-QUEUE-002
  - REQ-TASKS-RESTART-ORPHAN-SESSIONS-003
  - REQ-TASKS-PRIMARY-SESSION-OWNERSHIP-001
  - REQ-TASKS-DEFERRED-START-STEP-ENTRY-001
system_design:
  - ../../specs/tasks/system-design/resume-prompt-queue.md
  - ../../specs/tasks/system-design/restart-orphaned-session-terminalization.md
  - ../../specs/tasks/system-design/primary-session-ownership.md
  - ../../specs/tasks/system-design/deferred-start-step-entry.md
legacy_specs: []
---

# Implementation Plan: Keep queued work, accepted starts, and primary ownership across recovery

## Overview

Four recovery paths lose work or pick the wrong owner after a restart, a stuck
resume, or a session change:

- A queued prompt whose dispatch hit an expired wait for its session's runtime
  is dropped as "lost".
- A task whose automatic step start was interrupted before it produced a
  session stays in `SCHEDULING`, because no startup pass lists it.
- A full-row session write from a stale copy makes an earlier primary session
  primary again, and automatic promotion can pick a parked or finished session.
- A move into a step without an agent launches the task's deferred start.

Each case is reproduced on the current main branch with unit-level fixtures.
Each work order fixes one case with the smallest change in the code that owns
it.

## Scope

### In scope

- Treat `context.DeadlineExceeded` as a retryable queued-dispatch failure.
- One startup pass over orphaned `SCHEDULING` tasks, with the exclusions in
  REQ-TASKS-RESTART-ORPHAN-SESSIONS-003, and the repository list it needs.
- Remove `is_primary` from the full-row session update, and route both automatic
  primary promotions through one selector.
- Keep the deferred-launch record when a `task.moved` entry targets a step
  without automatic start.
- The requirement and system-design amendments listed in the frontmatter.

### Out of scope

- Moving queued messages to a new primary session (issue #4117 and its ADR).
- A periodic sweep, timer, or retry loop for any of the four cases.
- Readiness recovery of a resumed `STARTING` session after restart (#3240).
- Rendered UI.

## Contract changes

- `resume-prompt-queue` gains requirement 002. The exclusion of an automatic
  retry of a failed resume still holds: the entry waits for the next existing
  trigger.
- `restart-orphaned-session-terminalization` gains requirement 003. Requirement
  002.2 still holds. The new pass starts only an accepted step entry that never
  delivered its prompt, and it never prompts a session with prompt history.
- New `primary-session-ownership` requirement and design. Workflow recipient
  selection stays in `workflow-profile-session-lifecycle`. Promotion keeps
  using `SetPrimarySession`, so queued messages stay with their session.
- New `deferred-start-step-entry` requirement and design. Step prompts and start
  races stay in `workflow-step-agent-start-ownership`.

Both new pairs are separate files because the two existing system designs are
at their size limit.

## Technical approach

Work order 01 adds `errors.Is(err, context.DeadlineExceeded)` to the retryable
set in `handleQueuedMessageExecutionError`. The entry uses the existing requeue
branch.

Work order 02 adds `ListSchedulingTasks` to the SQLite repository and
`reconcileSchedulingOrphansOnStartup` to the orchestrator. The startup
lifecycle sweep calls it before `reconcileTaskLifecycleTokens`. The pass reaches
the repository through a narrow interface assertion, reuses the existing token,
deferred-launch, launch-error, Office, and dependency checks, and starts work
through `autoStartTaskForStep` or `processOnEnter`. A session with prompt
history only gets a compare-and-set state repair.

Work order 03 drops `is_primary` from `updateTaskSessionWithSnapshotGuard` and
adds `bestPrimarySessionCandidate` in a new `primary_session_candidate.go`.
`maybePromotePrimary` passes `activeOnly=true`, and
`promoteNextPrimaryAfterRemoval` passes `false`. Existing fixtures that set the
flag through the full-row write switch to `SetSessionPrimary`.

Work order 04 moves the step read in `autoStartTaskForStep` before the
deferred-launch call and adds `retainDeferredLaunch` to
`autoStartTaskForLoadedStep`. The other callers pass `false`.

## Tests

- Work order 01: AC-TASKS-RESUME-PROMPT-QUEUE-002.1 to 002.3 in an
  orchestrator queue test.
- Work order 02: AC-TASKS-RESTART-ORPHAN-SESSIONS-003.1 to 003.4 in
  orchestrator startup tests on a real SQLite repository, and a repository test
  for `ListSchedulingTasks`.
- Work order 03: AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.1 in a repository test,
  001.2 to 001.4 in orchestrator promotion tests.
- Work order 04: AC-TASKS-DEFERRED-START-STEP-ENTRY-001.1 to 001.3 in
  orchestrator auto-start tests.

Each test fails on the current main branch before its fix.

## E2E tests

No rendered UI changes. The defects are in backend recovery and selection
paths. Repository tests run on the real SQLite schema, and orchestrator tests
drive the real entry points with the existing test doubles. No browser scenario
is added.

## Work orders

- [x] [Task 01: Keep queued messages after an expired runtime wait](task-01-requeue-expired-runtime-wait.md)
- [ ] [Task 02: Restart interrupted automatic starts](task-02-restart-interrupted-automatic-starts.md)
- [ ] [Task 03: Keep one usable primary session](task-03-single-usable-primary-session.md)
- [ ] [Task 04: Keep the deferred start on an agentless step](task-04-agentless-step-deferred-start.md)

## Verification results

Not implemented yet.

## Risks

- Work order 02 starts work at backend startup. It is limited to accepted step
  entries that never delivered a prompt, and every start passes the ceiling and
  WIP admission. A deployment that relies on `SCHEDULING` tasks staying idle
  after restart would see them start.
- Work order 03 changes which session becomes primary when several are
  eligible. A task with no matching session still gets a primary.
- Work orders 02 to 04 change `service.go`, `task.go`, `session.go`,
  `task_operations.go`, and `event_handlers_workflow.go`, which other open pull
  requests also change. Whichever lands second is rebased onto the other.
