---
id: "03-single-usable-primary-session"
title: "Keep one usable primary session"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-PRIMARY-SESSION-OWNERSHIP-001
acceptance_criteria:
  - AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.1
  - AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.2
  - AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.3
  - AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.4
system_design:
  - ../../specs/tasks/system-design/primary-session-ownership.md
---

# Task 03: Keep one usable primary session

## Summary

A stale full-row session write can no longer make an earlier session primary
again, and automatic promotion prefers a session the current step can use.

## In scope

- Write failing repository and orchestrator tests first.
- Remove `is_primary` from the full-row `UPDATE` in
  `updateTaskSessionWithSnapshotGuard`.
- Add `bestPrimarySessionCandidate` in a new `primary_session_candidate.go`
  and call it from `maybePromotePrimary` (`activeOnly=true`) and
  `promoteNextPrimaryAfterRemoval` (`activeOnly=false`).
- Switch existing fixtures that set the primary flag through the full-row write
  to `SetSessionPrimary`.

## Out of scope

- Moving queued messages to the new primary (#4117).
- A unique index on `is_primary`.

## Acceptance

- After a stale copy of a demoted session is written, the task has one primary.
- A stop promotes a compatible active session over a parked one, and never an
  idle or terminal session. A deletion never promotes a terminal session.
- With only unmatched sessions left, one still becomes primary.

## Verification

From the repository root:

```bash
(cd apps/backend && go test -trimpath -tags fts5 ./internal/task/repository/sqlite -run 'Primary' -count=1)
(cd apps/backend && go test -trimpath -tags fts5 ./internal/orchestrator -run 'Primary' -count=1)
```

## Likely files

- `apps/backend/internal/task/repository/sqlite/session.go`
- `apps/backend/internal/orchestrator/primary_session_candidate.go`
- `apps/backend/internal/orchestrator/event_handlers_streaming.go`
- `apps/backend/internal/orchestrator/task_operations.go`
- Existing fixtures that write `IsPrimary` through `UpdateTaskSession`

## Results

- RED before the fix: `TestUpdateTaskSessionKeepsPrimaryFromStaleCopy` found
  two primaries; `TestPrimaryPromotionAfterStopSkipsParkedSession`,
  `TestPrimaryPromotionAfterStopPrefersCurrentStepProfile`, and
  `TestPrimaryPromotionAfterRemovalSkipsTerminalSession` picked the parked,
  other-profile, and completed sessions. The two guard tests passed.
- GREEN after the fix: all six tests pass.
- Two fixtures set the primary flag through the full-row write and now call
  `SetSessionPrimary`: `TestCreateNewSessionForStepKeepsCurrentSessionWhenWorkspaceAttachFails`
  and `newPreparedPromptFixture`. Every test in the orchestrator files that
  write `IsPrimary` before `UpdateTaskSession`, and the `mcp/handlers`
  message tests, pass.
- Production code changes the flag only through `SetSessionPrimary*`; the
  three places that assign `IsPrimary` do so after that call.
- In `sqlite`, `TestCancelActiveTaskSessionsByCandidatesRejectsNewActivityAndTurn`
  and `TestUpdateTaskSessionIfCurrentSnapshotRejectsSameStateProgress` fail
  intermittently on Windows with and without this change (4 of 8 runs on
  main): two writes inside one clock tick get the same `updated_at`.
