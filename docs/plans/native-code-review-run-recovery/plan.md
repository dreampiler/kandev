---
created: 2026-10-13
status: implemented
requirements:
  - REQ-AGENTS-REVIEW-RUN-RECOVERY-001
system_design:
  - ../../specs/agents/system-design/native-code-review-run-recovery.md
legacy_specs: []
---

# Implementation Plan: Native Review Run Recovery

## Overview

Make a native code-review pass always converge on a recorded terminal state,
stop one abandoned pass from making a task's Review surface permanently
unavailable, and let a pass that overruns its time budget fall back once to the
dynamic route's next candidate. The change is limited to the review runner
(`internal/review`) and its use of the existing review service write path. It
adds no queue, watcher, schema, or product requirement beyond the acceptance
criteria it implements.

## Confirmed root cause

Reproduced from `apps/backend/internal/review/runner.go`,
`apps/backend/internal/task/service/review_service.go`, and
`apps/backend/internal/task/repository/sqlite/task_review.go`:

- A pass runs under `runCtx = context.WithTimeout(backgroundContext(), runTimeout)`
  (`runTimeout = 10m`). When inference exceeds it, `execute` calls `Runner.fail`,
  which decides the run failed (a deadline is recorded as `cancelled`).
- `fail` already writes on a detached context
  (`context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)`), so the
  write does not expire with the request. The remaining gap is that it is a
  single attempt: when the database writer is busy past 10s, `FailRun` returns
  `context deadline exceeded`, `fail` only logs `record review run failure`, and
  the row stays `running`. The observed incident is exactly this: two reads plus
  one write inside the 10s budget lost to contention.
- The success path is weaker still: `execute` returns the `CompleteRun` error
  without recording any outcome, so a failed completion write also leaves the
  row `running`.
- Duplicate suppression is status-only. `ListActiveTaskReviewRuns` selects
  `status IN ('pending','running')` with no age or progress filter, and
  `launch` returns any such run unchanged. So the stranded row blocks every
  later `task.review.run` for the task until a process restart.
- A restart does clear it: `CancelInFlightTaskReviewRuns`
  (`internal/backendapp/helpers.go`) marks `pending`/`running` rows `cancelled`
  with `review_interrupted` / "interrupted by restart" on boot. This is a
  backstop, not a live-process guarantee, so it neither cleared the observed run
  nor unblocked the task before a restart.
- The pass budget is a fixed `runTimeout = 10m` and a deadline is recorded as
  `review_cancelled` with no fallback, so a slow free model that cannot finish a
  review in ten minutes ends the request outright even when the review-dynamic
  route holds another candidate that would finish. Failures clustered on one PR
  (#266, about 140 changed lines) show diff size is not the trigger.

Candidate comparison:

- (a) Detached terminal write plus bounded retry: the detached context already
  exists; adding bounded retry directly closes the observed transient-write
  failure. Chosen as Task 01.
- (b) Startup orphan cleanup: already implemented; kept unchanged.
- (c) Progress-less time limit excluded from duplicate suppression: the live
  guarantee for a permanently failed write. Chosen as Task 02, applied lazily at
  the existing suppression seam rather than as a new monitor.
- (d) Record a pass that overran its budget as `failed` (`review_timeout`) and
  retry once on the dynamic route's next candidate: recovers the timeout case
  the other two cannot, without changing route order or adding state. Chosen as
  Task 03. A diff-size-proportional budget is left as a follow-up candidate
  because the observed timeouts were on small diffs.

## Scope

### In scope

- Retry a pass's terminal write (`FailRun` and `CompleteRun`) on a detached,
  bounded context with shutdown-aware delays.
- End a failed `CompleteRun` as a recorded `failed` run instead of leaving it
  `running`.
- Treat a `pending`/`running` run older than the progress-less time limit as
  abandoned: record it `failed` with a reason and exclude it from duplicate
  suppression so a new pass starts.
- Record a pass that ends on its time budget as `failed` with a `review_timeout`
  reason, and retry exactly once on the dynamic route's next candidate, keeping
  the route order and never retrying a non-timeout failure or a retry.

### Out of scope

- The restart-time sweep and its cancelled/interrupted reason (unchanged).
- The duplicate-suppression query shape, review execution, prompt building,
  batching, and finding publication.
- Any new background monitor, queue, metric, schema column, or configuration.
- Changing the pass timeout (`runTimeout`) or the review API and events.
- A pass timeout proportional to the diff size (follow-up candidate).

## Technical approach

Task 01 introduces a terminal-write helper in `internal/review` that wraps a
write closure in a bounded retry loop. Each attempt gets a fresh
`context.WithTimeout(context.WithoutCancel(ctx), …)`; the delay between attempts
waits on a timer that also selects the runner's shutdown context, matching the
repository's goroutine-ownership rules. `Runner.fail` routes `FailRun` through
it; `execute` routes `CompleteRun` through it and, when every attempt fails,
falls through to `fail` so the run is recorded `failed`.

Task 02 adds a `staleRunLimit` constant greater than `runTimeout` and an
abandonment predicate. In `launch`, after the in-memory slot is claimed and an
active run is returned by the store, an abandoned run is finalized through the
Task 01 helper with a stable code, and the branch continues to create a new pass
instead of returning the abandoned run. The existing boot sweep is untouched.

Task 03 adds a `review_timeout` error code, records a deadline failure with it
(and a message naming the pass budget) instead of the generic cancelled code,
and adds a fallback in `execute`: when the reviewer error is a deadline and the
pass is not itself a retry, it resolves the timed-out identity's dynamic route
for the next candidate excluding the timed-out profile, creates one retry run on
the held in-flight slot, and runs it. The retry identity links back through the
existing run summary and error message. It reuses `nextDynamicReviewer` from the
existing dynamic launch fallback and introduces no new state.

## Work orders

- [done] [Task 01: Durable terminal write](task-01-terminal-write-durability.md)
- [done] [Task 02: Finalize abandoned run](task-02-finalize-abandoned-run.md)
- [done] [Task 03: Timeout fallback to next candidate](task-03-timeout-fallback.md)

## Dependency order

```text
Task 01 -> Task 02
Task 01 -> Task 03
```

Task 02 reuses the terminal-write helper from Task 01 to finalize the abandoned
run. Task 03 reuses it to record the timeout and reuses the existing
`nextDynamicReviewer` fallback from the dynamic launch path. All three ship in
one PR.

## Verification strategy

- Unit tests in `internal/review` prove the retry (write fails then succeeds),
  the exhausted-retry fallback to `failed`, unchanged single-attempt behavior,
  and the abandonment predicate/branch with a fake store.
- Unit tests in the same package prove a fresh active run is still rejoined and
  a terminal run is untouched.
- Unit tests prove the timeout fallback: a deadline records `review_timeout` and
  retries exactly once on the next dynamic candidate; no next candidate, a
  non-timeout failure, and a retry that also times out start no further pass; and
  an ordinary dynamic success starts none.
- `go build ./...` and `go vet` on the changed packages.
- On Windows, exclude the six non-loopback listener tests that prompt for
  firewall access if any broader run is used:
  `TestWaitForHealthBypassesHTTPProxyForSpecificBind`,
  `TestPortAvailableReportsAWildcardListenerAsOccupied`,
  `TestPortAvailableAtHostReportsNonLoopbackListener`,
  `TestNewInstanceConfigMCPURLsReachConfiguredListener`,
  `TestFindFreePortOnWildcardHostIsAvailableAtEveryInterface`,
  `TestEnsurePortAvailableChecksConfiguredListenHost`.
  None are in the changed packages.
- No repo-wide suite, no new E2E, no CI change.

## Verification results

- `go test ./internal/review -count=1` passed (full package), including the eight
  companion tests from Tasks 01-02, the five from Task 03, and six from the
  step-entry review fixes.
- `go build ./...` and `go vet ./internal/review ./internal/task/service
  ./internal/task/repository/sqlite` passed.
- `gofmt -l` clean; `golangci-lint run ./internal/review --new-from-rev=<base>`
  reported 0 issues.
- `python3 scripts/list-docs.py validate` and
  `python3 scripts/lint-spec-files.py --all` passed.

## Step-entry review (2026-10-13)

Reviewed range `ef4924f08d..d918afd5a3`. The step-entry review run
`7782693a-7f2d-4c25-87dc-5b19f7547cf1` (model `muse-spark-1.3-contributor`)
reported two findings, both acted on:

- The timeout fallback ran even when the timed-out run's finalization write had
  failed, which could leave two active rows for the task. Fixed: `fail` now
  records through `recordFailure`, which reports whether the row reached a
  terminal state, and the fallback is gated on that; companion test
  `TestRunner_TimeoutDoesNotRetryWhenFinalizationFails`.
- The `ActiveRun` read fell through to `CreateRun` on a read error. Fixed:
  `launch` fails closed on that read; companion test
  `TestRunner_ActiveRunReadErrorFailsClosed`.

A third classification issue found during the same pass: a storage write that
timed out on a detached context was labeled `review_timeout` with a "pass
exceeded its budget" message. Fixed by keying the timeout code on the pass
context deadline only; companion test
`TestRunner_CompletionWriteDeadlineKeepsExecutionFailureReason`.

## Second step-entry review (2026-10-13)

Reviewed range `ef4924f08d..85a8902c19` (head `85a8902c19`). The step-entry
review run `314b5cd7-d195-4ba1-9f2c-d89259cc2371` (model
`muse-spark-1.3-contributor`) reported one finding:

- A pass whose deadline expired while `PublishFindings` ran was recorded as
  `review_timeout` but got no candidate fallback, unlike an inference-phase
  timeout, so `review_timeout` meant two different things. Fixed: the publish
  failure now records through `recordFailure` and takes the same one-shot
  fallback; `PublishFindings` runs on the pass context, so a deadline there is
  the pass overrunning its budget. Companion test
  `TestRunner_PublishPhaseTimeoutRetriesNextCandidate`.

## Third step-entry review (2026-10-13)

Reviewed range `85a8902c19..bbab4a70e8` (head `bbab4a70e8`). The step-entry
review run `9dde6fff-d256-4d18-8abe-df2b876332c8` (model
`muse-spark-1.3-contributor`) reported two findings, both acted on:

- `recordFailure` dereferenced a nil `cause` before the guard `fail` kept for it.
  The base `fail` dereferenced `cause` in its persist block the same way, so this
  is not a behavior change; the guard is nonetheless restored so a nil cause is
  treated as nothing to record. Companion test
  `TestRunner_RecordFailureNilCauseDoesNotPanic`.
- A completion-write failure whose retries outlasted the pass context was
  recorded as `review_timeout`. The pass-budget decision is now taken before the
  completion write's retries, so a storage failure keeps its execution-failure
  reason. Companion test `TestRunner_CompletionWriteFailureIsNotAPassTimeout`.

## Risks

- Retrying a terminal write must stay idempotent: `mutateRunIfLive` already
  leaves a terminal row unchanged, so a retry after a partially applied write is
  safe, but tests must confirm the retry is bounded and does not loop forever.
- The abandonment limit must exceed `runTimeout` with margin so a live pass is
  never misclassified; keep the margin in one named constant.
- Delays must select on shutdown so `Runner.Stop` is not delayed by backoff.
- `execute`'s new fallback from `CompleteRun` to `fail` must not overwrite a
  user cancel; `mutateRunIfLive` keeps the cancelled row.
- The timeout fallback must not loop: it is suppressed when the pass already is a
  retry, and it must preserve the route's candidate order rather than choosing a
  model itself. A user cancel stays `cancelled`; only a pass deadline becomes
  `review_timeout`.

## Package handoff

The package is the requirement pair (the new companion requirement plus the new
system design) and the work orders above. Implementation starts after the
orchestrator's instruction and follows the dependency order, updating each work
order's status and this plan as its verification passes.
