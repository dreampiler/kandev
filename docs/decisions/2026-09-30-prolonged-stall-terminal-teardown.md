# ADR-2026-09-30-prolonged-stall-terminal-teardown: A Prolonged Silent Prompt Is Terminal and Returns Its Ceiling Slot

**Status:** accepted
**Date:** 2026-09-30
**Area:** backend

## Context

[ADR-2026-07-29-agent-stall-user-controlled-recovery](2026-07-29-agent-stall-user-controlled-recovery.md)
kept stall recovery user-controlled: a prompt that has produced a turn event and
then goes quiet stays `RUNNING` indefinitely, because event silence is not proof
that a long-running tool is safe to kill. The never-started exception
([ADR-2026-08-18](2026-08-18-never-started-agent-stall-terminal.md),
[ADR-2026-09-02](2026-09-02-terminal-stall-owns-process-teardown.md)) only covers
a prompt that emitted zero turn events since dispatch.

Operationally that gap is not benign. A provider connection that drops without a
timeout leaves the ACP `Prompt` call open and the process alive while emitting no
turn events. The session row sits in `RUNNING`/`STARTING` forever, holding a slot
in the session ceiling. Enough of these silently saturate the ceiling, so every
new automatic launch is refused and the backend appears to hang even though no
single component is dead. The observed recovery cadence (manual restart every
~1h) is the symptom: `waitForPromptDone` (`apps/backend/internal/agent/runtime/lifecycle/session.go`)
logs an advisory stall at five minutes but never advances past it, the
stuck-signal watchdog only reclaims sessions holding a pending completion
signal, and the task-level stall sweep waits two hours.

The fix is a bounded, automatic exception to user-controlled recovery: a
`RUNNING`/`STARTING` prompt that has produced no turn event for a prolonged
period is terminal. It must be disposed and its ceiling slot returned.

## Decision

1. **A prolonged silent prompt is terminal.** After 15 minutes without a turn
   event or user input, the prompt is classified terminal even though it
   previously produced turn events. The lifecycle injects a synthetic completion
   signal so `waitForPromptDone` returns, and requests forced teardown of the
   process. The session settles out of `RUNNING`/`STARTING`, which returns its
   ceiling slot through the existing reservation accounting.
2. **The five-minute advisory notice is unchanged.** A prompt that reaches five
   minutes of silence still gets the neutral running notice and the Cancel turn
   action. The 15-minute terminal classification is a later, separate step, not
   a replacement for it. Between five and fifteen minutes the operator still
   controls recovery; only at fifteen minutes does recovery become automatic.
3. **The honest inactivity clock is the source of truth.** The same
   prompt-progress clock that feeds the five-minute watchdog feeds the
   15-minute terminal classification. Only a turn event or user input restarts
   it; metadata frames do not. A prompt that is genuinely producing turn output
   is never classified terminal.
4. **Terminal handling is record-then-teardown, like the never-started path.**
   The orchestrator records the terminal outcome first, then requests forced
   teardown of the execution. A failed teardown leaves the recorded state
   authoritative and the execution registered for a later stop.

## Consequences

A dropped provider connection no longer wedges a session row until the operator
restarts the backend. After 15 minutes the row is reclaimed, the ceiling slot is
returned, and the launch path drains again.

This reverses, for the prolonged case only, the "never automatically
kills/cancels a started turn" rule from
[ADR-2026-07-29](2026-07-29-agent-stall-user-controlled-recovery.md). A
legitimate long-running tool that emits no turn event for 15 minutes is now torn
down; that ambiguity is accepted as the cost of recovering from a silent
connection drop, and the 15-minute bound (3x the advisory threshold) keeps the
false-positive window large relative to normal turn cadence.

The bounded `neverStartedStopTimeout` pattern (30s detached force teardown)
generalizes to this path so a hung daemon cannot wedge the teardown goroutine.

## Alternatives Considered

- **Keep recovery fully operator-controlled and document the restart loop.**
  Rejected. The restart cadence is the observed failure mode, and the ceiling
  saturation it produces blocks *all* automatic launches, not just the stuck
  session.
- **Lower the threshold to the five-minute advisory mark.** Rejected. That would
  collapse the distinction between the advisory notice and terminal disposal and
  shrink the false-positive window to the point that a slow-but-legitimate tool
  is routinely killed.
- **Reuse the stuck-signal watchdog by synthesizing a completion signal.**
  Rejected. That watchdog is keyed to a durable `step_complete_kandev` signal and
  deliberately advances the workflow step; a silent prompt has no signal to
  apply and must not advance a step it never asked for.
- **Add a timeout to the unbounded ACP `Prompt` call instead.** Insufficient on
  its own. A deadline on the adapter call bounds the goroutine but does not by
  itself classify the prompt, record a terminal state, or return the ceiling
  slot; it is upstream of the observation and disposal this ADR owns. It may be
  evaluated separately as defence in depth.
