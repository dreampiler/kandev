---
status: draft
system: agents
requirements:
  - REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001
---

# Prolonged Stall Inactivity Teardown System Design

## Purpose and boundaries

The agent system owns the prompt watchdog clock, its classification, and the
terminal teardown of a stalled execution. This design extends that same
watchdog: after 15 minutes of honest inactivity, a silent prompt becomes
terminal rather than advisory, and its execution is torn down so the session
leaves `RUNNING`/`STARTING` and returns its ceiling slot.

It does not own the persisted session/task state machine, the notice rendering,
or the ceiling reservation accounting. It reuses the orchestrator's existing
terminal-transition and teardown seams, the honest clock from
[agent stall recovery](agent-stall-recovery.md), and the ceiling accounting from
[session concurrency ceiling](session-concurrency-ceiling.md).

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001` | [Prompt watchdog](#prompt-watchdog), [Terminal classification](#terminal-classification), [Teardown and settlement](#teardown-and-settlement) |

## Components and responsibilities

- **`AgentExecution` activity state** (`internal/agent/runtime/lifecycle`): the
  existing honest prompt-progress clock, discriminator, and activity epoch.
  Reused unchanged.
- **The prompt watchdog** in `waitForPromptDone` (`session.go`): the one-minute
  ticker that snapshots the clock. It gains a second threshold — 15 minutes —
  that classifies the prompt terminal instead of merely advisory.
- **The orchestrator stall handler** (`event_handlers_stall.go`): validates the
  snapshot's ownership and, for the prolonged case, records the terminal outcome
  and requests teardown through the same record-then-teardown path the
  never-started case uses.
- **The agent manager's stop entry point** (`StopAgentWithReason`): the forced,
  bounded teardown. Reused unchanged.
- **The session-ceiling controller**: already releases a reservation when the
  session leaves `RUNNING`/`STARTING`; no change is required, the settlement
  below is what triggers it.

## Data and contracts

The published stall payload already carries task, session, and execution
identity, prompt generation, last-activity time, elapsed duration, activity
epoch, and the `NeverStarted` discriminator. This design does not change that
payload's shape for the advisory case.

The terminal classification is carried the same way the never-started case is:
a new signal on the stall payload, or an extension of the existing event, that
tells the orchestrator "terminal, tear down". The exact field is an
implementation detail constrained by the existing `AgentStalledPayload` and its
ownership guards.

## Control flow

```text
one-minute ticker
  -> snapshot clock / discriminator / epoch
  -> elapsed < 5m            : nothing
  -> 5m <= elapsed < 15m     : publish advisory stall (existing), once per generation
  -> elapsed >= 15m          : publish terminal stall, once per generation
       -> orchestrator validates ownership (execution, generation, epoch)
       -> record terminal outcome (session/task leave RUNNING/STARTING)
       -> request forced, bounded teardown of the execution
       -> ceiling controller releases the slot when the row settles
```

The 15-minute classification uses the same ownership guards as the advisory and
never-started cases: a stale execution, a superseded prompt generation, or a
bumped activity epoch rejects the terminal event. A prompt that emitted a turn
event after the snapshot was taken never reaches the terminal branch.

### Prompt watchdog

`waitForPromptDone` already ticks once per minute and snapshots
`promptActivitySnapshot()`. The five-minute advisory branch stays. A new
`>= 15m` branch, checked after the advisory branch, publishes the terminal
stall and does not publish again for the same generation. The synthetic
completion signal injected here unblocks the in-flight `waitForPromptDone`
waiter so the prompt path returns rather than blocking on `promptDoneCh`
forever.

### Terminal classification

Classification is a pure read of the snapshot. The 15-minute bound applies to a
prompt that has produced turn events; the existing five-minute never-started
classification remains separate and unchanged.

### Teardown and settlement

Terminal handling is record-then-teardown, mirroring the never-started path:
record the terminal outcome first so it stays authoritative, then request forced
teardown on a detached, bounded context. Teardown uses `StopAgentWithReason`
with `force: true`, which stops the runtime and deregisters the execution
without writing a cancellation state over the recorded outcome.

Settlement removes the session from `RUNNING`/`STARTING` (for example to a
`FAILED` or input-ready terminal state carrying the terminal message). The exact
terminal state and its error copy follow the never-started precedent so the
user sees a terminal failure rather than a hung running card.

## Failure and recovery

- A teardown failure is logged with task, session, and execution identity. The
  recorded terminal state stays authoritative and the execution remains
  registered for a later stop or startup cleanup.
- A late turn event between the snapshot and the handler bumps the activity
  epoch and the terminal event is rejected, so a prompt that resumed is not torn
  down.
- An already-removed execution reports not-found on teardown, which is success.
- The unbounded ACP `Prompt` call is not made bounded here; the bounded teardown
  is what closes the consequence, as in the never-started decision.

## Persistence

No schema change. The clock, discriminator, and epoch stay in memory for the
execution's lifetime. The durable record is the existing session state, session
error message, task state, and notice message.

## Security

No new user input path and no new externally reachable operation. The terminal
classification is driven by the same trusted lifecycle clock as the existing
watchdog. Log fields keep their existing bounds: identifiers and durations only.

## Observability

A new terminal-teardown log line carries task, session, and execution identity
plus the elapsed duration and the teardown outcome, so a reclaimed prompt and a
failed teardown are greppable. The existing per-generation single-stall log is
retained for the advisory case. Where the repository already emits
`workflow_*`/stall metrics, the terminal classification may add a labelled
counter with a closed label set (no task, session, or agent identifier as a
label).

## Related decisions

- [ADR-2026-09-30-prolonged-stall-terminal-teardown](../../../decisions/2026-09-30-prolonged-stall-terminal-teardown.md)
- [ADR-2026-07-29-agent-stall-user-controlled-recovery](../../../decisions/2026-07-29-agent-stall-user-controlled-recovery.md)
- [ADR-2026-09-02-terminal-stall-owns-process-teardown](../../../decisions/2026-09-02-terminal-stall-owns-process-teardown.md)
- [Session concurrency ceiling](session-concurrency-ceiling.md)
