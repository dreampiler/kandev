---
status: current
system: office
requirements:
  - REQ-OFFICE-CONSUMED-WAKE-001
---

# Office Consumed Children-Completed Wake System Design

## Purpose and boundaries

The Office scheduler's pre-launch staleness pass gains one more cancellation
reason. It applies to children-completed runs whose parent has already finished.
The design adds two narrow reads and reuses the existing cancellation path. It
adds no outcome, no ledger, and no producer change.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-OFFICE-CONSUMED-WAKE-001` | [Where the check runs](#where-the-check-runs), [The decision](#the-decision) |

## Where the check runs

`evaluateRunStaleness` (`office/service/scheduler_staleness.go`) runs on every
`processRun` pass. It runs after the pause, agent-status, and agent-lookup gates,
and before checkout, budget admission, and launch.

A run parked under `waiting_for_session_capacity` is queued but not claimable.
`LiftParkedRuns` clears its block, the next claim picks it up, and it re-enters
`processRun`. A parent that finished while the run waited is therefore caught
before the relaunch.

The new check runs ahead of the existing workflow-step comparison. A parent that
both finished and left its step is cancelled either way, and the recorded reason
names the real cause.

## The decision

1. **Scope the run.** If the run's reason is neither `task_children_completed`
   nor the legacy `children_completed`, or its payload has no `task_id`, the
   check does nothing.
2. **Read the parent's state.** The check calls `GetTaskState`
   (`office/repository/sqlite/tasks.go`). A missing row reads as `""`, which is
   not terminal. A read error is returned to the caller.
3. **Check for terminal.** A parent that is neither `COMPLETED` nor `CANCELLED`
   leaves the run untouched. These are the two states the backstop's
   `ListStuckParents` excludes.
4. **Check for live coalesced wakes.** `HasCoalescedWakeupOutsideReasons`
   (`office/repository/sqlite/wakeup_requests.go`) is one `EXISTS` query over
   `agent_wakeup_requests`. It reports whether any request with status
   `coalesced` and this `run_id` has an effective reason, `COALESCE(NULLIF(reason, ''), source)`,
   outside `routine_dispatch_cron` and `heartbeat`. That is the same fallback the
   wakeup dispatcher applies. If such a request exists, the run launches.
5. **Cancel.** Otherwise `cancelStaleRun` cancels the run with reason
   `parent_task_terminal`. It releases any checkout, clears the agent's working
   mark, writes `cancelled`, records the terminal shape, publishes the processed
   event, and logs `run_cancelled_stale`.

The terminal predicate is reused rather than redefined. The check sits outside
every producer, so readiness and wave identity are untouched.

## Failure and recovery

A state or wakeup read error returns through the staleness error path, which
hands the run to `HandleRunFailure` for its ordinary retry. A cancel that loses
a race to another writer publishes nothing, exactly as the existing stale
cancel does.

## Verification obligations

Companion tests in `office/service/scheduler_staleness_children_completed_test.go`
cover the following:

- a `COMPLETED` and a `CANCELLED` parent cancel without a launch;
- a live parent launches;
- a run carrying only periodic coalesced wakes is cancelled;
- a run carrying an event wake launches;
- a ceiling-parked run whose parent finished while it waited is cancelled once
  lifted.
