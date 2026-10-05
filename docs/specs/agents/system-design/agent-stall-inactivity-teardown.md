---
status: current
system: agents
created: 2026-09-30
updated: 2026-10-05
requirements:
  - REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001
---

# Prolonged Stall Inactivity Teardown System Design

## Purpose and boundaries

The agent system owns the prompt watchdog clock, its classification, and the
terminal teardown of a stalled execution. This design covers the prolonged case:
once honest inactivity passes the execution's escalation threshold, a silent prompt
that previously produced turn events becomes terminal, and its execution is torn
down so the session leaves `RUNNING`/`STARTING` and returns its ceiling slot.

The threshold itself is not owned here. When a top-level tool is executing the
threshold is the longer foreground-tool allowance, and the evidence that refreshes
it belongs to
[foreground tool progress](agent-stall-recovery.md#foreground-tool-progress) under
`REQ-AGENTS-TOOL-STALL-PROGRESS-001`. This design owns what happens once the
threshold is crossed.

It does not own the persisted session/task state machine, the notice rendering, or
the ceiling reservation accounting. It reuses the orchestrator's existing
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
- **`AgentExecution.stallThreshold()`** (`tool_progress.go`): returns the
  escalation threshold for the current open-tool state. Reused unchanged.
- **The prompt watchdog** in `waitForPromptDone` (`session.go`): the one-minute
  ticker that snapshots the clock. It gains a terminal branch at the escalation
  threshold that classifies the prompt instead of merely advising.
- **The stall publisher** (`stall_publish.go`): publishes the classification with a
  bounded wait and returns whether the synchronous handler completed. Reused
  unchanged.
- **The orchestrator stall handler** (`event_handlers_stall.go`): validates the
  snapshot's ownership and, for the prolonged case, records the terminal outcome
  and requests teardown through the same record-then-teardown path the
  never-started case uses.
- **The agent manager's stop entry point** (`StopAgentWithReason`): the forced,
  bounded teardown. Reused unchanged.
- **The session-ceiling controller**: already releases a reservation when the
  session leaves `RUNNING`/`STARTING`; no change is required, the settlement below
  is what triggers it.

## Data and contracts

The published stall payload already carries task, session, and execution identity,
prompt generation, last-activity time, elapsed duration, activity epoch, and the
`NeverStarted` discriminator. This design does not change that payload's shape for
the advisory case.

The terminal classification is a sibling discriminator on the same payload:
`AgentStalledPayload.ProlongedStall`, serialized as `prolonged_stall` and omitted
when false. It is mutually exclusive with `NeverStarted`, and the lifecycle only
sets it for a prompt that produced turn events, so a never-started prompt is never
reclassified.

## Control flow

```text
one-minute ticker
  -> snapshot clock / discriminator / epoch / open-tool revision
  -> elapsed < 5m                          : nothing
  -> 5m <= elapsed, below threshold        : publish advisory stall (existing), once per generation
  -> elapsed >= threshold, no open tool    : prolonged -> terminal
  -> elapsed >= threshold, tool executing  : 45m allowance; refreshed by validated progress
       -> publish terminal stall, once per generation (bounded publish wait)
       -> orchestrator validates ownership (execution, generation, epoch, tool revision)
       -> record terminal outcome (session/task leave RUNNING/STARTING)
       -> request forced, bounded teardown of the execution
       -> ceiling controller releases the slot when the row settles
```

The classification uses the same ownership guards as the advisory and never-started
cases: a stale execution, a superseded prompt generation, a bumped activity epoch,
or a changed open-tool revision rejects the terminal event. A prompt that emitted a
turn event after the snapshot was taken never reaches the terminal branch.

### Prompt watchdog

`waitForPromptDone` already ticks once per minute and snapshots
`promptActivitySnapshot()`. The five-minute advisory branch stays. A branch at
`elapsed >= execution.stallThreshold()`, checked after the advisory branch,
publishes the terminal stall and does not publish again for the same generation. A
per-generation latch resets only when the activity epoch changes, so a genuinely
resumed prompt can still be classified later in a new epoch.

The synthetic completion injected here releases the in-flight prompt wait so the
prompt path returns rather than blocking on `promptDoneCh` forever. It is
generation-checked and the channel is buffered size 1, so a real completion that
arrived first wins.

### Terminal classification

Classification is a pure read of the snapshot compared against the execution's
current threshold. The five-minute never-started classification remains separate
and unchanged, and the two never both apply to one prompt.

### Teardown and settlement

Terminal handling is record-then-teardown, mirroring the never-started path:
record the terminal outcome first so it stays authoritative, then request forced
teardown on a detached, bounded context. `Service.recordAndStopStalledExecution`
performs the recording and delegates to `Service.stopStalledExecution`, which is
the shared seam for both terminal classifications. Teardown uses force, which stops
the runtime and deregisters the execution without writing a cancellation state over
the recorded outcome.

Settlement removes the session from `RUNNING`/`STARTING` with the terminal message
the user sees. The exact terminal state and its error copy follow the never-started
precedent so the user sees a terminal failure rather than a hung running card. The
prolonged case carries its own error and stop reason
(`errAgentProlongedStall`, `prolongedStallStopReason`) and its own notice text, so
the two classifications stay distinguishable in logs and in the UI.

## Failure and recovery

- A publish that exceeds the bounded wait is logged and does not block the prompt
  indefinitely; the terminal outcome is what matters, not an unbounded wait behind
  a stalled handler.
- A teardown failure is logged with task, session, and execution identity. The
  recorded terminal state stays authoritative and the execution remains registered
  for a later stop or startup cleanup.
- A late turn event between the snapshot and the handler bumps the activity epoch
  and the terminal event is rejected, so a prompt that resumed is not torn down.
- An already-removed execution reports not-found on teardown, which is success.
- The unbounded ACP `Prompt` call is not made bounded here; the bounded teardown is
  what closes the consequence, as in the never-started decision.

## Persistence

No schema change. The clock, discriminator, epoch, and open-tool state stay in
memory for the execution's lifetime. The durable record is the existing session
state, session error message, task state, and notice message.

## Security

No new user input path and no new externally reachable operation. The terminal
classification is driven by the same trusted lifecycle clock as the existing
watchdog. Log fields keep their existing bounds: identifiers and durations only.

## Observability

The escalation log line carries execution identity, elapsed time, and the
never-started flag, so a reclaimed prompt is greppable, and the teardown outcome is
reported with task, session, and execution identity. The existing per-generation
single-stall log is retained for the advisory case. No new metric is required: the
classification reuses the existing stall log lines, and any future counter must use
a closed label set with no task, session, or agent identifier as a label.

## Related decisions

- [ADR-2026-09-30-prolonged-stall-terminal-teardown](../../../decisions/2026-09-30-prolonged-stall-terminal-teardown.md)
- [ADR-2026-07-29-agent-stall-user-controlled-recovery](../../../decisions/2026-07-29-agent-stall-user-controlled-recovery.md)
- [ADR-2026-09-02-terminal-stall-owns-process-teardown](../../../decisions/2026-09-02-terminal-stall-owns-process-teardown.md)
- [Session concurrency ceiling](session-concurrency-ceiling.md)
- [Agent stall recovery](agent-stall-recovery.md)