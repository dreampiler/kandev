---
status: draft
system: agents
created: 2026-10-01
owners:
  - kandev
---

# Agent Update Stream Continuity Requirements

## Overview

An ACP process can keep working while the backend stops receiving its turn
events. An unexpected update-stream disconnect currently fails an execution;
a stream that remains open but stops delivering events can only be classified
by the prompt inactivity watchdog. The agent system owns the runtime stream,
prompt generation, event delivery, and the evidence needed to distinguish a
silent agent from a broken delivery stage. Task state and queued follow-up
delivery remain owned by the task system.

## Requirements

### REQ-AGENTS-UPDATE-STREAM-CONTINUITY-001: Preserve and diagnose active prompts across stream faults

**Intent:** Restore event delivery to a live agent when its backend update
stream fails, and make the first missing delivery stage identifiable when a
connection appears healthy but no turn events arrive.

#### Acceptance criteria

- **AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.1:** When an update stream closes
  unexpectedly during an active prompt and the same agentctl instance remains
  reachable, Kandev shall make a bounded reattachment attempt for that exact
  execution and prompt generation without resending the prompt or starting a
  second agent process.
- **AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.2:** An event or terminal outcome
  recovered after reattachment shall be applied at most once to its original
  prompt. A stale connection or superseded execution shall not settle a newer
  prompt or session.
- **AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.3:** If reattachment or terminal
  outcome reconciliation fails within the bound, the execution shall enter its
  existing recoverable failure path instead of remaining indefinitely
  `RUNNING`. An intentional stop, cancellation, credential rotation, or backend
  shutdown shall not trigger reattachment.
- **AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.4:** At the first inactivity
  classification for a prompt, Kandev shall emit one bounded diagnostic record
  that distinguishes the last adapter-to-agentctl event, agentctl-to-backend
  transport receipt, backend stream read, and backend event-handler progress
  where those stages are observable. A missing or unavailable stage shall be
  stated as unavailable, not as proof that the agent was silent. The record
  shall contain no prompt, event body, credential, or raw protocol frame.
- **AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.5:** Transport liveness signals
  and metadata frames shall not count as turn progress or reset the existing
  five- and fifteen-minute inactivity thresholds. A blocked event handler
  shall not prevent the stream reader from receiving later protocol responses.

## Compatibility and exclusions

The current stall notice, terminal inactivity teardown, session ceiling, and
task recovery contracts remain in force. This requirement does not restart the
whole Kandev installation, replay a prompt to the provider, or infer provider
failure from a WebSocket that is merely quiet. It does not change user-facing
controls or expose a new public API.

## Related contracts

- [Agent stall recovery](agent-stall-recovery.md) owns the inactivity clock
  and existing classifications.
- [Prolonged stall teardown](agent-stall-inactivity-teardown.md) owns the
  fifteen-minute terminal consequence and is not reimplemented here.
- [Queued post-dispatch recovery](../../tasks/requirements/queued-post-dispatch-recovery.md)
  owns follow-up queue progress after an interrupted turn.

## Implementation plans

- [Agent stream stall and queue recovery](../../../plans/agent-stream-stall-and-queue-recovery/plan.md)
