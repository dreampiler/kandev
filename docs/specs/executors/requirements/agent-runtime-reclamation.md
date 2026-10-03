---
status: active
system: executors
created: 2026-10-03
owners:
  - kandev
---

# Agent Runtime Reclamation Requirements

## Overview

An installation accumulates agent processes and committed memory across operating
rounds and backend restarts. On the host observed on 2026-10-03, agentctl
descendants reached roughly 12.1 GB committed across about 106 processes, and host
commit reached 79.9 of 90.9 GB. Earlier rounds showed the same shape at 9.5, 8.0,
and 7.5 GB with up to 232 processes, and recovering required restarting the
backend, which interrupted live work.

The earlier hypothesis named completed sessions as the cause. Measurement on the
operating install refuted it: `Completed` was already inside the reclaim set. The
retained runtimes belong to sessions in `FAILED` and `CANCELLED`.

## Measured evidence

Read from the operating install's SQLite database with a read-only connection. No
command lines, credentials, or resume tokens were read or recorded.

Of the `executors_running` rows the installation still considers live (a non-zero
`local_pid`):

- 21 rows carry a live runtime handle.
- 7 of those 21 belong to sessions whose `task_sessions.state` is terminal. All 7
  are `status='running'` against a `FAILED` session. The oldest went `FAILED` on
  2026-09-27 and was still marked `running`.
- The other 14 belong to sessions in `RUNNING` or `WAITING_FOR_INPUT`, which is
  the expected cost of live work.
- Idle suspension was active and had fired: 22 rows carry
  `idle_suspension_state='suspended'`.

Instance ports are reused across rounds: of 35 distinct ports that appear in the
inventory, 31 are shared by more than one session, with up to 24 rows on a single
port. Accumulation is therefore per round, not bounded by distinct ports.

## Why the enabled idle policy missed them

The per-workspace idle-suspension preference was enabled in every workspace at a
60 minute timeout. It is not the failure. The reclaim predicate that both reclaim
paths share admitted only `WaitingForInput`, `Idle`, and `Completed`, and excluded
`Failed` and `Cancelled` on the stated belief that startup reconciliation and the
cancel pipelines already reconcile those rows.

That belief does not hold for a session that fails at runtime:

- Startup reconciliation observes a terminal session only if the session was
  already terminal when the backend booted. A session that moves `RUNNING` to
  `FAILED` while the backend keeps running is never seen by it again.
- A session that fails never enters the cancel pipeline, which only handles a user
  cancellation.

The result is a session that cannot receive a message, still holding a full agent
process tree and a live `executors_running` row, for the life of the installation.

## Terminology

- **Terminal session:** a session in `Completed`, `Failed`, or `Cancelled`. It
  cannot receive a message, so no runtime behind it has a remaining reader.
- **Reclaim:** release the provider-runtime reservation backing a session and
  repair its `executors_running` row, preserving the resume token.
- **Recovery inventory:** the set of live `executors_running` rows offered to
  startup recovery as re-adoption candidates.

## Requirements

### REQ-EXECUTORS-RUNTIME-RECLAIM-001: Terminal-session runtime teardown

The reclaim predicate shall admit `Failed` and `Cancelled` sessions.

For a terminal session the live-runtime guard shall not block reclaim: an agent
process still alive behind a session that cannot receive a message is precisely
the retained footprint reclaim exists to release. Every other fail-closed guard
continues to apply:

- An active turn still blocks reclaim, so a turn genuinely in flight is protected.
- The resume token is preserved, so the session remains resumable.
- The worktree path is preserved.
- `Running`, `Starting`, and `Created` remain excluded; they are not terminal.

Acceptance criteria:

- **AC-EXECUTORS-RUNTIME-RECLAIM-001.1:** A `Failed` or `Cancelled` session whose liveness probe reports a live agent process yields disposition `reclaimed`, and its `executors_running` row reaches `status=stopped`.
- **AC-EXECUTORS-RUNTIME-RECLAIM-001.2:** A `Failed` or `Cancelled` session with an active turn yields disposition `skipped_active_turn`.
- **AC-EXECUTORS-RUNTIME-RECLAIM-001.3:** A `WaitingForInput` session reporting a live runtime still yields disposition `skipped_live_runtime`.
- **AC-EXECUTORS-RUNTIME-RECLAIM-001.4:** A `Running` or `Starting` session yields disposition `skipped_state`.
- **AC-EXECUTORS-RUNTIME-RECLAIM-001.5:** The reclaimed row preserves its resume token.

### REQ-EXECUTORS-RUNTIME-RECLAIM-002: Startup recovery excludes terminal sessions

A live `executors_running` row whose owning session is terminal shall not be
offered as a recovery re-adoption candidate.

Recovery correlation re-tracks every live instance that still has a record, so
keeping the record re-adopts the runtime for a session that can no longer receive
a message. Omitting the record makes the instance a record-less orphan, which the
existing correlation already stops. This is a change of input, not of correlation
policy.

The filter shall fail closed: a session that cannot be read is not evidence of a
terminal state, so its record is kept and existing recovery behaviour is unchanged.

Acceptance criteria:

- **AC-EXECUTORS-RUNTIME-RECLAIM-002.1:** The standalone recovery inventory omits rows whose owning session is `Completed`, `Failed`, or `Cancelled`.
- **AC-EXECUTORS-RUNTIME-RECLAIM-002.2:** The inventory retains rows whose owning session is `Running`, `WaitingForInput`, or `Idle`.
- **AC-EXECUTORS-RUNTIME-RECLAIM-002.3:** A session read that errors retains the row it belongs to.
- **AC-EXECUTORS-RUNTIME-RECLAIM-002.4:** An omitted record causes the owning instance to be classified as a record-less orphan and stopped by the existing recovery stop path.

### REQ-EXECUTORS-RUNTIME-RECLAIM-003: Authoritative per-session footprint

An operator shall be able to attribute live agent cost to the session that holds
it, on both desktop and phone.

The projection shall report, per live runtime: the owning session and task
identity, the runtime state, the number of owned live processes, committed bytes,
and resident bytes. Attribution shall be by instance and session identity, never
by process name or a bare process identifier.

A measurement that cannot read every owned process shall report the count it
could not read, and its byte totals shall then be read as a lower bound. A
platform that cannot report commit charge per process shall report zero committed
bytes rather than estimating from resident bytes.

The projection shall carry no transcript content, credential, resume token, or
command line.

Acceptance criteria:

- **AC-EXECUTORS-RUNTIME-RECLAIM-003.1:** Every projected row carries session identity, runtime state, owned process count, committed bytes, and resident bytes.
- **AC-EXECUTORS-RUNTIME-RECLAIM-003.2:** Every measured process is a descendant of the owning instance's own agent process, never a host-wide name match.
- **AC-EXECUTORS-RUNTIME-RECLAIM-003.3:** An owned process that cannot be measured is counted as unreadable, and a nonzero unreadable count is presented as making the byte totals a lower bound.
- **AC-EXECUTORS-RUNTIME-RECLAIM-003.4:** A platform that cannot report commit charge per process reports zero committed bytes rather than an estimate.
- **AC-EXECUTORS-RUNTIME-RECLAIM-003.5:** No task, session, execution, user, or agent identifier appears as an expvar label.
- **AC-EXECUTORS-RUNTIME-RECLAIM-003.6:** The phone view presents the same capability as the desktop view.

### REQ-EXECUTORS-RUNTIME-RECLAIM-004: Descendant reclamation completeness

Owned descendant processes shall be reclaimed when their owning runtime stops.

This requirement was already implemented and is retained here because it was a
live hypothesis during investigation. See "Evidence-closed requirements".

Acceptance criteria:

- **AC-EXECUTORS-RUNTIME-RECLAIM-004.1:** Releasing a managed agent's lifecycle handle terminates a descendant the agent spawned.
- **AC-EXECUTORS-RUNTIME-RECLAIM-004.2:** A descendant that outlives its leader is reaped before the owning manager reports it stopped.
- **AC-EXECUTORS-RUNTIME-RECLAIM-004.3:** The process runner and the VS Code manager do not return ownership while a descendant is still alive.

## Evidence-closed requirements

### Descendant reclamation is not the leak

The working hypothesis during investigation was that owned descendants survived
their leader, based on two observations: `processGroupAlive` returns `false`
unconditionally on Windows, and auxiliary processes appeared to use a different
stop group from the agent job object.

Both observations were checked and neither is a defect:

- `processGroupAlive` returning `false` on Windows is deliberate and documented.
  `taskkill /T` is the authoritative process-tree operation on that platform, and
  the function exists only to answer an "is this group still alive" question that
  is not asked there.
- Agent processes are launched suspended and bound to a kill-on-close Job Object
  before they can spawn anything, so the job is the ownership boundary.

Four Windows regression tests cover this and all pass on the observed host:

| Test | Asserts |
| --- | --- |
| `TestWindowsProcessLifecycleJobKillsDescendants` | releasing the job handle kills a spawned descendant |
| `TestWindowsManagedLifecycleReapsDescendantAfterLeaderExit` | a descendant outlives its leader and is reaped by job reap |
| `TestWindowsProcessRunnerReapsDescendantAfterLeaderExit` | the process runner does not return ownership before its descendant exits |
| `TestWindowsVscodeLifecycleReapsDescendantAfterLeaderExit` | the VS Code manager does not return ownership before its descendant exits |

Command and result:

```
go test ./internal/agentctl/server/process/ -run "TestWindowsProcessLifecycleJobKillsDescendants|TestWindowsManagedLifecycleReapsDescendantAfterLeaderExit|TestWindowsProcessRunnerReapsDescendantAfterLeaderExit|TestWindowsVscodeLifecycleReapsDescendantAfterLeaderExit" -count=1 -v
--- PASS: TestWindowsProcessLifecycleJobKillsDescendants (0.63s)
--- PASS: TestWindowsManagedLifecycleReapsDescendantAfterLeaderExit (0.96s)
--- PASS: TestWindowsProcessRunnerReapsDescendantAfterLeaderExit (0.72s)
--- PASS: TestWindowsVscodeLifecycleReapsDescendantAfterLeaderExit (0.73s)
ok  github.com/kandev/kandev/internal/agentctl/server/process  3.223s
```

The requirement is closed with evidence and no code change. Reopen it if a
descendant outside these ownership paths is ever observed surviving.

## Out of scope

- Changing the default of the idle-suspension preference, adding an
  always-on retention floor, or adding an ADR to justify either. The owner
  declined all three on 2026-10-03; the preference is already enabled in every
  operating workspace and was not the fault.
- Per-session hard memory caps. No single session exceeded about 0.8 GB; the
  defect is the number of retained runtimes, not the size of any one.
- Remote executor lifetimes (Docker, SSH, Kubernetes, Sprites, plugin), whose
  processes live on another host and have no host-local footprint to attribute.
- Provider quiescence and the operating session's temporary hang-check gate.

## Traceability

| Requirement | Work order | State |
| --- | --- | --- |
| REQ-EXECUTORS-RUNTIME-RECLAIM-001 | WO-01 | delivered |
| REQ-EXECUTORS-RUNTIME-RECLAIM-002 | WO-01 | delivered |
| REQ-EXECUTORS-RUNTIME-RECLAIM-003 | WO-02 | not started |
| REQ-EXECUTORS-RUNTIME-RECLAIM-004 | closed with evidence | closed |