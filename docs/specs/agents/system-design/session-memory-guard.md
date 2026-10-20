---
status: current
system: agents
requirements:
  - REQ-AGENTS-SESSION-CEILING-003
created: 2026-10-20
owners:
  - kandev
---

# Session Memory Guard Design

## Boundary and mapping

| Criteria | Implementation boundary |
| -------- | ----------------------- |
| .1, .2   | `sessionCeilingController.admit`, `handOffOrAdmit`, and `decideLocked` in `orchestrator/session_ceiling.go`; `memoryPressuredLocked` and `coordinatorTaskMemoryExemption` in `session_ceiling_memory.go`; the card note in `ceiling_surface.go` |
| .3       | The existing ceiling retry sweep, which replays deferred launches through the same admission |
| .4       | `memoryPressuredLocked` fail-open path and `logMemoryUnavailableLocked` |
| .5       | `features.sessionMemoryGuard`, `ServiceConfig.SessionMemoryGuardEnabled`, and `hostmemory.ReadConfig` |

## Host reading

`internal/system/hostmemory` is a leaf package that only observes the host.
Windows reads `GlobalMemoryStatusEx` for physical memory and psapi
`GetPerformanceInfo` for system-wide commit charge. Linux reads
`/proc/meminfo` (`MemAvailable`, `Committed_AS`, `CommitLimit`). Other
platforms return an incomplete reading. `Gate.Pressured` is pure: available
memory below the floor, or commit percent above the ceiling when commit is
known, is pressure; an incomplete reading is unknown.

## Admission

The controller reads the host inside its existing critical section, only when
the gate is enabled and the task is not exempt. A pressured host bypasses the
unlimited-ceiling fast path so that the population read can still recognize an
already-admitted session. `decideLocked` returns already-admitted sessions
first, then refuses with `memory_pressure` before the free-slot and manual
override branches, so the manual override never passes the memory condition.
The refusal reserves no capacity and carries no population reading.
`handOffOrAdmit` keeps its counted-session hand-off before the memory check.

A refused launch takes the existing deferral path, so the card note, queue
order, and sweep replay are shared with count refusals. The note names the
host condition.

## Exemption

`NewService` passes `coordinatorTaskMemoryExemption`, which exempts tasks whose
origin is the Office coordinator. A task that cannot be read is not exempt.

## Verification

Orchestrator unit tests drive the controller with a fake reader: automatic and
manual refusal, the unlimited ceiling, the commit ceiling, counted sessions and
hand-offs, the exemption, fail-open and its single warning, the disabled gate,
and re-admission after pressure clears. Hostmemory tests cover `Pressured`,
`ReadConfig`, and the live Windows reader.
