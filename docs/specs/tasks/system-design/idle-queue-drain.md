---
status: current
system: tasks
requirements:
  - REQ-TASKS-IDLE-QUEUE-DRAIN-001
created: 2026-10-20
owners:
  - kandev
---

# Idle Queue Drain Design

## Boundary and mapping

| Criteria | Implementation boundary |
| -------- | ----------------------- |
| .1, .2   | `orchestrator.Service.drainIdleQueues`, called from `ceilingSweepTick`, delegating to `DrainQueuedMessageIfAutoRun` |
| .3       | `idleQueueDrainBackoff` in `orchestrator/queue_idle_drain.go` |
| .4       | `messagequeue.Service.ListSessionsWithPending` (SQLite join on `task_sessions`) |

## Sweep pass

Each ceiling sweep tick, after deferred ceiling launches, lists sessions with
at least one queued row that is not reserved in flight. For each session whose
backoff is due it calls `DrainQueuedMessageIfAutoRun`, which already reads the
Auto-run policy, rejects non-promptable sessions, and holds the cancel/in-flight
guard; the dispatch then runs through the ordinary queued-dispatch path and its
recovery-block check. The pass never sets Auto-run.

## Backoff

The backoff is in memory and per session: the first attempt is immediate, then
one, two, four, eight, and fifteen minutes. Sessions missing from a listing are
forgotten, so a session that drains and later queues again starts fresh. A
restart clears the state, which is safe because the next listing re-derives it.

## Listing

`ListSessionsWithPending` is an optional repository capability implemented by
the memory and SQLite repositories. The SQLite query joins `task_sessions` so
rows left by a deleted session are not offered, and skips rows reserved by an
in-flight dispatch.

## Verification

Orchestrator tests cover dispatch from an idle session, a paused Auto-run, a
running session, and the backoff schedule. Messagequeue tests cover the listing
on both repositories and the deleted-session exclusion.
