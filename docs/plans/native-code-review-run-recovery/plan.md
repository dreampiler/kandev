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

Make a native code-review pass always converge on a recorded terminal state, and
stop one abandoned pass from making a task's Review surface permanently
unavailable. The change is limited to the review runner (`internal/review`) and
its use of the existing review service write path. It adds no queue, watcher,
schema, or product requirement beyond the two acceptance criteria it implements.

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

Candidate comparison:

- (a) Detached terminal write plus bounded retry: the detached context already
  exists; adding bounded retry directly closes the observed transient-write
  failure. Chosen as Task 01.
- (b) Startup orphan cleanup: already implemented; kept unchanged.
- (c) Progress-less time limit excluded from duplicate suppression: the live
  guarantee for a permanently failed write. Chosen as Task 02, applied lazily at
  the existing suppression seam rather than as a new monitor.

## Scope

### In scope

- Retry a pass's terminal write (`FailRun` and `CompleteRun`) on a detached,
  bounded context with shutdown-aware delays.
- End a failed `CompleteRun` as a recorded `failed` run instead of leaving it
  `running`.
- Treat a `pending`/`running` run older than the progress-less time limit as
  abandoned: record it `failed` with a reason and exclude it from duplicate
  suppression so a new pass starts.

### Out of scope

- The restart-time sweep and its cancelled/interrupted reason (unchanged).
- The duplicate-suppression query shape, review execution, prompt building,
  batching, and finding publication.
- Any new background monitor, queue, metric, schema column, or configuration.
- Changing the pass timeout (`runTimeout`) or the review API and events.

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

## Work orders

- [done] [Task 01: Durable terminal write](task-01-terminal-write-durability.md)
- [done] [Task 02: Finalize abandoned run](task-02-finalize-abandoned-run.md)

## Dependency order

```text
Task 01 -> Task 02
```

Task 02 reuses the terminal-write helper from Task 01 to finalize the abandoned
run. Both ship in one PR.

## Verification strategy

- Unit tests in `internal/review` prove the retry (write fails then succeeds),
  the exhausted-retry fallback to `failed`, unchanged single-attempt behavior,
  and the abandonment predicate/branch with a fake store.
- Unit tests in the same package prove a fresh active run is still rejoined and
  a terminal run is untouched.
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
  new companion tests.
- `go build ./...` and `go vet ./internal/review ./internal/task/service
  ./internal/task/repository/sqlite` passed.
- `gofmt -l` clean; `golangci-lint run ./internal/review --new-from-rev=<base>`
  reported 0 issues.
- `python3 scripts/list-docs.py validate` and
  `python3 scripts/lint-spec-files.py --all` passed.

## Risks

- Retrying a terminal write must stay idempotent: `mutateRunIfLive` already
  leaves a terminal row unchanged, so a retry after a partially applied write is
  safe, but tests must confirm the retry is bounded and does not loop forever.
- The abandonment limit must exceed `runTimeout` with margin so a live pass is
  never misclassified; keep the margin in one named constant.
- Delays must select on shutdown so `Runner.Stop` is not delayed by backoff.
- `execute`'s new fallback from `CompleteRun` to `fail` must not overwrite a
  user cancel; `mutateRunIfLive` keeps the cancelled row.

## Package handoff

The package is the requirement pair (the new companion requirement plus the new
system design) and the two work orders above. Implementation starts after the
orchestrator's instruction and follows the dependency order, updating each work
order's status and this plan as its verification passes.
