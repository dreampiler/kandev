---
id: "03-timeout-fallback"
title: "Timeout fallback to next candidate"
status: done
wave: 2
depends_on:
  - "01-terminal-write-durability"
plan: "plan.md"
requirements:
  - REQ-AGENTS-REVIEW-RUN-RECOVERY-001
acceptance_criteria:
  - AC-AGENTS-REVIEW-RUN-RECOVERY-001.5
system_design:
  - ../../specs/agents/system-design/native-code-review-run-recovery.md
---

# Task 03: Timeout Fallback To Next Candidate

## Summary

A review pass whose reviewer overruns the pass timeout ends the request with no
recorded timeout reason and no second attempt. Record the run `failed`
(`review_timeout`), then retry exactly once on the dynamic route's next
candidate, in the route's own order, excluding the candidate that timed out.

## In scope

- Add a `review_timeout` error code and record a deadline failure with it and a
  message naming the pass budget, instead of the generic cancelled code.
- When a reviewer pass fails on its deadline, resolve the timed-out identity's
  dynamic route for the next candidate (excluding the timed-out profile and any
  origin match) and run one retry pass on the held in-flight slot.
- Link the retry to the run it replaces through the existing run summary and
  failure message; no new column or table.
- Suppress the fallback when the pass already is a retry, when the identity is
  not a dynamic-route candidate, or when the route has no other candidate.
- Trigger only on a deadline; a parse failure, an unavailable agent, or any other
  reviewer error is recorded and stops.

## Out of scope

- Changing the pass timeout value, the route order, or the `nextDynamicReviewer`
  selection rule.
- Retrying more than once, or retrying a non-timeout failure.
- A pass timeout proportional to the diff size (follow-up candidate).
- Frontend changes; the run history already renders the failed reason.

## Acceptance

- A pass that times out records `review_timeout` and starts exactly one retry on
  the next dynamic candidate, which is linked to the timed-out run.
- No other candidate: the timed-out run is recorded and nothing else starts.
- A non-timeout reviewer error: recorded failed, no retry.
- A retry that also times out: recorded and never retried again.
- A dynamic reviewer that succeeds on the first attempt: no retry.

## Verification

Write the failing tests first, then implement. From `apps/backend`:

```bash
go test ./internal/review -run 'Timeout|Retry|Fallback' -count=1
go build ./...
go vet ./internal/review
```

## Files likely touched

- `apps/backend/internal/review/runner.go`
- `apps/backend/internal/review/errors.go`
- `apps/backend/internal/review/runner_timeout_fallback_test.go` (new test file)

## Dependencies

- Task 01's terminal-write helper (used to record the timeout).
- The existing `Resolver.nextDynamicReviewer` fallback from the dynamic launch
  path.

## Risks

- The fallback must be one shot; suppress it on a retry and cover that with a
  test.
- The route order and exclusion rule are the existing resolver's responsibility;
  the runner must not pick a model itself.
- A user cancel is `context.Canceled`, not `DeadlineExceeded`, and must stay
  `cancelled`; only a deadline becomes `review_timeout`.

## Parallelism

`sequential`

## Inputs

- `REQ-AGENTS-REVIEW-RUN-RECOVERY-001`.
- `docs/specs/agents/system-design/native-code-review-run-recovery.md`.
- Task 01's terminal-write helper and the resolver's dynamic fallback.

## Results

- Added `CodeTimeout = "review_timeout"`; `Runner.fail` records it with a message
  naming the pass budget, while a user cancel still stays `cancelled`.
- `execute` now calls a one-shot fallback on a deadline: it skips the timeout
  when the request already is a retry, resolves the route's next candidate
  through `nextDynamicReviewer` (excluding the timed-out profile), creates one
  retry run on the held slot, and links it through the run summary and failure
  message.
- New tests: `TestRunner_TimeoutRetriesNextDynamicCandidate`,
  `TestRunner_TimeoutWithoutNextCandidateDoesNotRetry`,
  `TestRunner_NonTimeoutFailureDoesNotRetry`,
  `TestRunner_RetryTimeoutDoesNotRetryAgain`,
  `TestRunner_DynamicReviewerSuccessDoesNotRetry`.
- Verified with
  `go test ./internal/review -run 'TimeoutRetriesNextDynamicCandidate|TimeoutWithoutNextCandidateDoesNotRetry|NonTimeoutFailureDoesNotRetry|RetryTimeoutDoesNotRetryAgain|DynamicReviewerSuccessDoesNotRetry' -count=1`.
