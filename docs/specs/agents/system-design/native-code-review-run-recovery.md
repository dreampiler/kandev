---
status: current
system: agents
requirements:
  - REQ-AGENTS-REVIEW-RUN-RECOVERY-001
---

# Native review run recovery System Design

## Purpose and boundaries

A native code-review pass runs in the background while its request returns
immediately. The pass must always converge on a recorded terminal state, and one
abandoned pass must never make a task's Review surface permanently unavailable.

This design owns the write durability of a run's terminal outcome and the
treatment of a run that stays non-terminal past the pass's own time budget. It
does not own the review execution substrate, prompt building, batch planning, or
finding publication; those stay with the native code review runner
(`internal/review`). It does not own the restart-time sweep that already closes
runs left in flight by a stopped process; that behavior stays with the backend
startup path.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-REVIEW-RUN-RECOVERY-001` | [Failure and recovery](#failure-and-recovery) |

## Components and responsibilities

- `review.Runner` (`apps/backend/internal/review/runner.go`) owns a pass: it
  claims a task's single in-flight slot, deduplicates against an existing run,
  creates the pending run row, drives execution under a bounded context, and
  records the terminal outcome.
- `taskservice.ReviewService` (`apps/backend/internal/task/service/review_service.go`)
  is the write path for run and finding state. `FailRun`, `CompleteRun`, and
  `CancelRun` move a run out of `pending`/`running`; the `mutateRunIfLive`
  guard leaves an already-terminal run unchanged.
- The SQLite repository
  (`apps/backend/internal/task/repository/sqlite/task_review.go`) persists the
  run row and exposes `ListActiveTaskReviewRuns`, the status-only query that
  backs duplicate suppression.
- The backend startup path (`apps/backend/internal/backendapp/helpers.go`)
  calls `CancelInFlightTaskReviewRuns` once on boot. That sweep remains the
  restart boundary; this design does not replace it.

## Control flow

1. A caller (`task.review.run`, the `run_code_review` step action, or MCP)
   invokes `Runner.Launch`.
2. `launch` claims the task's in-memory slot, then asks the store for the task's
   active run. When one exists it is returned unchanged (duplicate
   suppression); otherwise the runner resolves identity and changes, creates a
   `pending` run, and starts the pass goroutine.
3. The goroutine marks the run `running` and executes the review on a context
   bounded by the pass timeout (`runTimeout`).
4. The pass ends in one terminal write: `CompleteRun` on success, or `FailRun`
   through `Runner.fail` on any failure.

The terminal write is the last durable step and the only point where a run
leaves `running`. It must therefore not depend on the liveness of the request or
the pass context, and it must survive a transient storage error.

## Failure and recovery

Two independent guarantees keep a run from being stranded as `running`.

### Detached, bounded terminal write

The terminal write uses a context detached from the pass
(`context.WithoutCancel`) with its own short timeout, so the pass's own deadline
or a caller disconnect cannot cancel the write that clears `running`. Because a
single detached write can still lose to a busy writer, `Runner` retries it a
bounded number of times with short delays. Each attempt uses a fresh detached
context; delays select on the runner's shutdown signal so a stop does not wait
on a backoff. The same retry wraps the success path: a `CompleteRun` whose write
ultimately fails falls through to a recorded `Failed` outcome instead of leaving
the row `running`.

### Progress-less time limit

A live pass cannot outlive its bounded execution context. A run that is still
`pending` or `running` past a limit greater than that budget therefore has no
live goroutine behind it. Such a run is abandoned: its row's progress fields
(`duration_ms`, token counts) are written only by `CompleteRun`, so a stranded
row always shows zero progress.

The limit is `2 * runTimeout` (20 minutes against the 10-minute pass timeout),
defined once as `staleRunLimit` in `internal/review`. The margin is deliberately
wide: it must be long enough that a pass the runner still owns, including one
inside its bounded terminal-write retry, is never finalized as abandoned, and
short enough that a genuinely stranded run stops blocking the task well within
an operator's session. A run with no recorded `created_at` is never classified
as abandoned, so an unknown age fails open to the existing rejoin behavior.

The runner applies the limit at the duplicate-suppression seam. When
`launch` finds an active run that is older than the limit, it first records that
run `failed` with a stable abandonment reason and then proceeds to create the
new pass, so a new review request for the same task is never blocked by an
abandoned one. This is a lazy check at an existing decision point, not a new
background monitor. The restart sweep in the startup path remains the backstop
for a run that no request ever observes again.

### Existing cancellation and restart behavior

- A user cancel cancels the live pass context and records `cancelled`; a late
  completion does not overwrite it (`mutateRunIfLive`).
- A process restart closes any `pending`/`running` row as `cancelled` with the
  restart reason before new work starts.

## State transitions

| From | To | Trigger |
| --- | --- | --- |
| - | `pending` | review requested |
| `pending` | `running` | the pass claims the diff and dispatches the reviewer |
| `running` | `completed` | reviewer result parsed and findings persisted |
| `running` | `failed` | reviewer error, unparseable result, a failed terminal write, or the progress-less time limit |
| `pending`/`running` | `cancelled` | user cancel, or the backend start sweep |

Every terminal state is final. The progress-less time limit and the retry both
target a run that would otherwise remain non-terminal.

## Persistence

No schema change. The abandoned-run reason reuses `error_code` and
`error_message` and a `completed_at` timestamp, like every other terminal
outcome. The abandoned run stays in the task's run history as a `failed` row so
the reason remains visible.

## Observability

The runner already logs a failed pass and a terminal-write failure. This design
adds one log when an abandoned run is finalized at the suppression seam, naming
the run and task and the abandonment reason. No new metric, queue, or watcher is
introduced.

## Related decisions

- Run finalization reuses the atomic status transition owned by
  [Native review finding status atomicity](native-code-review-status-atomicity.md).
- This recovery contract is owned by
  [Native Review Run Recovery](../requirements/native-code-review-run-recovery.md);
  the review outcome and duplicate-suppression baseline are owned by
  [Native Code Review](../requirements/native-code-review.md).
