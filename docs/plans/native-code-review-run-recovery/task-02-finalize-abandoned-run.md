---
id: "02-finalize-abandoned-run"
title: "Finalize abandoned run"
status: done
wave: 2
depends_on:
  - "01-terminal-write-durability"
plan: "plan.md"
requirements:
  - REQ-AGENTS-REVIEW-RUN-RECOVERY-001
acceptance_criteria:
  - AC-AGENTS-REVIEW-RUN-RECOVERY-001.3
  - AC-AGENTS-REVIEW-RUN-RECOVERY-001.4
system_design:
  - ../../specs/agents/system-design/native-code-review-run-recovery.md
---

# Task 02: Finalize Abandoned Run

## Summary

A `pending`/`running` run older than the progress-less time limit has no live
goroutine behind it. Record it `failed` with a reason and exclude it from
duplicate suppression so a new review request starts instead of rejoining it.

## In scope

- Add a named `staleRunLimit` constant greater than `runTimeout`, with the
  margin kept in one place.
- Add an abandonment predicate for a non-terminal run older than the limit.
- In `launch`, after the in-memory slot is claimed and the store returns an
  active run, finalize an abandoned run through the Task 01 terminal-write
  helper using a stable abandonment code, then continue to create the new pass
  instead of returning the abandoned run.
- Log the abandonment with run and task identifiers and the reason.
- Keep `ListActiveTaskReviewRuns` and the restart sweep unchanged.

## Out of scope

- Any background monitor, queue, or periodic sweep.
- Changing the duplicate-suppression SQL or the pass timeout.
- The cleanup of abandoned rows beyond the `failed` status and reason.
- Frontend changes; the existing run list already renders a failed run reason.

## Acceptance

- A store returning an active run older than the limit: `Launch` records it
  `failed` with the abandonment reason and creates a new run for the request.
- An active run within the limit is still rejoined unchanged with no write.
- An already-terminal run is untouched.

## Verification

Write the failing tests first, then implement. From `apps/backend`:

```bash
go test ./internal/review -run 'Abandoned|Stale|Dedup|Rejoin' -count=1
go build ./...
go vet ./internal/review
```

## Files likely touched

- `apps/backend/internal/review/runner.go`
- `apps/backend/internal/review/runner_abandoned_test.go` (new test file)

## Dependencies

- Task 01's terminal-write helper.

## Risks

- The limit must exceed `runTimeout` with margin so a live pass is never
  finalized; assert the margin in a test.
- Finalization must not turn a user cancel into a failure; `mutateRunIfLive`
  preserves the cancelled row.
- The branch must free the claimed slot on every path.

## Parallelism

`sequential`

## Inputs

- `REQ-AGENTS-REVIEW-RUN-RECOVERY-001`.
- `docs/specs/agents/system-design/native-code-review-run-recovery.md`.
- Task 01's terminal-write helper.

## Results

- Added `staleRunLimit = 2 * runTimeout` (20 minutes) and an `abandoned`
  predicate that never classifies a terminal run or a run with no recorded age.
- `launch` now finalizes an abandoned active run through the terminal-write
  helper with `review_abandoned` and starts the requested pass; if the
  abandonment cannot be recorded, it keeps the single-active-run invariant and
  returns the existing run.
- Added `CodeAbandoned = "review_abandoned"`; the startup sweep is unchanged.
- Verified with `go test ./internal/review -run 'TestRunner_AbandonedRunIsFinalizedAndNewReviewStarts|TestRunner_FreshActiveRunIsRejoined|TestRunner_ActiveRunWithUnknownAgeIsRejoined|TestRunner_TerminalRunIsNotAbandoned|TestRunner_AbandonedRunStaysWhenFinalizeFails' -count=1`.
