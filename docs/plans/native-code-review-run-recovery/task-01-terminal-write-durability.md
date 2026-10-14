---
id: "01-terminal-write-durability"
title: "Durable terminal write"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-REVIEW-RUN-RECOVERY-001
acceptance_criteria:
  - AC-AGENTS-REVIEW-RUN-RECOVERY-001.1
  - AC-AGENTS-REVIEW-RUN-RECOVERY-001.2
system_design:
  - ../../specs/agents/system-design/native-code-review-run-recovery.md
---

# Task 01: Durable Terminal Write

## Summary

Record a review pass's terminal outcome even when the first write attempt loses
to a busy database writer, so a run cannot be stranded in `running` by a
transient storage failure.

## In scope

- Add a bounded terminal-write retry helper in `internal/review`. Each attempt
  uses a fresh `context.WithTimeout(context.WithoutCancel(ctx), …)`; the delay
  between attempts waits on a timer that also selects the runner's shutdown
  context.
- Route `Runner.fail`'s `FailRun` call through the helper.
- Route `execute`'s `CompleteRun` call through the helper, and when every
  attempt fails, fall through to `fail` so the run is recorded `failed` instead
  of staying `running`.
- Keep the existing cancel/deadline classification in `fail` unchanged.

## Out of scope

- The progress-less time limit and duplicate-suppression change (Task 02).
- The restart-time `CancelInFlightTaskReviewRuns` sweep.
- New metrics, configuration, or schema.
- The review service's `mutateRunIfLive` guard contract.

## Acceptance

- A store whose terminal write fails the first attempts and then succeeds
  records the terminal outcome; the write does not observe the cancelled pass
  context.
- A `CompleteRun` that fails on every attempt leaves a `failed` run row, never a
  `running` one.
- A write that succeeds on the first attempt performs no retry and no delay.

## Verification

Write the failing tests first, then implement. From `apps/backend`:

```bash
go test ./internal/review -run 'TerminalWrite|Retry|Complete|Fail' -count=1
go build ./...
go vet ./internal/review ./internal/task/service
```

## Files likely touched

- `apps/backend/internal/review/runner.go`
- `apps/backend/internal/review/runner_terminal_write_test.go` (new test file;
  keep within the 800-effective-line limit rather than appending to an existing
  large test file)

## Dependencies

None.

## Risks

- Retry loops must be bounded and must not delay shutdown; select on the runner
  context between attempts.
- A retry after a partially applied write must remain safe; `mutateRunIfLive`
  already leaves a terminal row unchanged.
- The `CompleteRun` fallback must not overwrite a user cancel.

## Parallelism

`sequential`

## Inputs

- `REQ-AGENTS-REVIEW-RUN-RECOVERY-001`.
- `docs/specs/agents/system-design/native-code-review-run-recovery.md`.

## Results

- Added a `terminalWritePolicy` on `Runner` and a `writeTerminal` helper that
  retries a terminal write on a fresh `context.WithoutCancel` context per
  attempt, with a shutdown-aware delay.
- Routed `Runner.fail`'s `FailRun` and `execute`'s `CompleteRun` through it; a
  completion write that fails on every attempt now falls through to `fail`, so
  the run is recorded `failed` with `review_execution_failed` instead of staying
  `running`.
- Verified with `go test ./internal/review -run 'TestRunner_TerminalWriteRetriesOnDetachedContext|TestRunner_CompletionWriteFailureRecordsFailedRun|TestRunner_NormalCompletionWritesOnce' -count=1`.
