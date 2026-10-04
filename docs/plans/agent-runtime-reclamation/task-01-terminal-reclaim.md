---
id: "01-terminal-reclaim"
title: "Terminal-session reclaim and recovery exclusion"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-EXECUTORS-RUNTIME-RECLAIM-001
  - REQ-EXECUTORS-RUNTIME-RECLAIM-002
  - REQ-EXECUTORS-RUNTIME-RECLAIM-004
acceptance_criteria:
  - AC-EXECUTORS-RUNTIME-RECLAIM-001.1
  - AC-EXECUTORS-RUNTIME-RECLAIM-001.2
  - AC-EXECUTORS-RUNTIME-RECLAIM-001.3
  - AC-EXECUTORS-RUNTIME-RECLAIM-001.4
  - AC-EXECUTORS-RUNTIME-RECLAIM-001.5
  - AC-EXECUTORS-RUNTIME-RECLAIM-002.1
  - AC-EXECUTORS-RUNTIME-RECLAIM-002.2
  - AC-EXECUTORS-RUNTIME-RECLAIM-002.3
  - AC-EXECUTORS-RUNTIME-RECLAIM-002.4
  - AC-EXECUTORS-RUNTIME-RECLAIM-004.1
  - AC-EXECUTORS-RUNTIME-RECLAIM-004.2
  - AC-EXECUTORS-RUNTIME-RECLAIM-004.3
system_design:
  - ../../specs/executors/system-design/agent-runtime-reclamation.md
---

# WO-01 — Terminal-Session Reclaim and Recovery Exclusion

**State:** delivered
**Requirements:** REQ-EXECUTORS-RUNTIME-RECLAIM-001, REQ-EXECUTORS-RUNTIME-RECLAIM-002, REQ-EXECUTORS-RUNTIME-RECLAIM-004 (evidence close)
**Branch:** `kd/agent-runtime-terminal-reclaim`

## Problem

A session that reaches `Failed` or `Cancelled` while its agent process is alive
keeps that runtime and a live `executors_running` row for the life of the
installation, and every backend restart re-adopts it.

Measured on the operating install: 7 of 21 live-claimed rows belong to terminal
sessions, all `status='running'` against a `FAILED` session, the oldest since
2026-09-27. The idle-suspension preference was enabled everywhere and had fired
22 times; it was not the fault.

Two independent causes, fixed separately.

## Change 1 — reclaim predicate admits terminal sessions

`internal/orchestrator/reconcile_liveness.go`, `classifyIdleReclaim`.

The predicate admitted `WaitingForInput`, `Idle`, `Completed` and excluded
`Failed` and `Cancelled`, on the stated belief that startup reconciliation and the
cancel pipelines already reconcile those rows. Startup reconciliation only
observes a session that was already terminal at boot, and a failed session never
enters the cancel pipeline, so neither covers this case.

Terminal states are now admitted, and the live-runtime guard applies only to open
sessions. The active-turn guard still applies to every state, so a turn genuinely
in flight is never released.

### Tests

`internal/orchestrator/reclaim_lifecycle_test.go`:

- `TestClassifyIdleReclaimDisposition` — matrix rows for a `Failed` and a
  `Cancelled` session with the liveness probe still reporting the process alive
  (want `reclaimed`), and a `Failed` session with an active turn (want
  `skipped_active_turn`).
- `TestReclaimIdleSessionReleasesTerminalSession` — new. Drives
  `reclaimIdleSession` end to end for both terminal states with
  `isAgentRunning: true` and asserts the row reaches `status=stopped`.
- `TestReclaimIdleSessionRefusesWrongState` — the `failed` and `cancelled` rows
  were removed from this table. This test pinned the old exclusion, which is the
  contract being changed, so the rows moved to the new test above rather than
  being deleted.

Observed red before the change:

```
--- FAIL: TestClassifyIdleReclaimDisposition/failed_session_reclaims_even_though_a_runtime_probe_still_sees_the_process
    disposition = "skipped_state", want "reclaimed"
--- FAIL: TestClassifyIdleReclaimDisposition/cancelled_session_reclaims_even_though_a_runtime_probe_still_sees_the_process
    disposition = "skipped_state", want "reclaimed"
--- FAIL: TestClassifyIdleReclaimDisposition/failed_session_with_an_active_turn_is_still_skipped
    disposition = "skipped_state", want "skipped_active_turn"
```

## Change 2 — recovery inventory excludes terminal sessions

`internal/agent/runtime/lifecycle/persistence.go`,
`ListLiveStandaloneExecutorsRunning`.

A live row whose owning session is terminal is no longer offered as a
re-adoption candidate. Without its record, the instance is a record-less orphan,
which the existing correlation already stops. Correlation policy is unchanged.

The filter fails closed: an unreadable or missing session keeps its record.

### Tests

`internal/agent/runtime/lifecycle/executor_running_live_standalone_test.go`:

- `TestListLiveStandaloneExecutorsRunningDropsTerminalSessions` — six sessions
  across `Running`, `Failed`, `Cancelled`, `Completed`, `WaitingForInput`, `Idle`;
  asserts the three open states survive and the three terminal states are dropped.
- `TestListLiveStandaloneExecutorsRunningKeepsRecordWhenSessionUnreadable` — a
  reader that errors keeps the record.

Observed red before the change:

```
--- FAIL: TestListLiveStandaloneExecutorsRunningDropsTerminalSessions
    s-failed: kept in the recovery inventory but its session is terminal
    s-cancelled: kept in the recovery inventory but its session is terminal
    s-completed: kept in the recovery inventory but its session is terminal
```

## Change 3 — invariant comments that are no longer true

`internal/orchestrator/idle_session_reaper.go` asserted "never stops a live
process" and that "a live executor is short-circuited inside reclaimIdleSession
before any side effect". Both described the old predicate. Rewritten to state the
current invariant: a terminal session is released through `reclaimIdleSession`, a
dead row takes the repair path, and a live executor on a resumable session is
still short-circuited.

## Change 4 — descendant reclamation: closed with evidence, no code change

The hypothesis that owned descendants survive their leader was checked and
rejected. `processGroupAlive` returning `false` on Windows is deliberate and
documented; `taskkill /T` is the authoritative tree operation there. Agent
processes are launched suspended and bound to a kill-on-close Job Object before
they can spawn anything.

Four existing Windows regression tests cover the behaviour and all pass:

```
--- PASS: TestWindowsProcessLifecycleJobKillsDescendants (0.63s)
--- PASS: TestWindowsManagedLifecycleReapsDescendantAfterLeaderExit (0.96s)
--- PASS: TestWindowsProcessRunnerReapsDescendantAfterLeaderExit (0.72s)
--- PASS: TestWindowsVscodeLifecycleReapsDescendantAfterLeaderExit (0.73s)
ok  github.com/kandev/kandev/internal/agentctl/server/process  3.223s
```

No production change was made for REQ-004. If a descendant outside these
ownership paths is ever observed surviving, reopen it.

## Verification

```
go test ./internal/orchestrator/ -run "TestClassifyIdleReclaim|TestReclaimIdleSession" -count=1
ok  github.com/kandev/kandev/internal/orchestrator

go test ./internal/agent/runtime/lifecycle/ -run "TestListLiveStandaloneExecutorsRunning" -count=1
ok  github.com/kandev/kandev/internal/agent/runtime/lifecycle
```

Full-package `internal/orchestrator` is not clean on the base commit either, with
a different order-dependent failure set per run. The table in `plan.md` records
the comparison. The reclaim tests pass deterministically in isolation.

## Not changed

No setting, no default, no migration, no wire contract, no web UI.