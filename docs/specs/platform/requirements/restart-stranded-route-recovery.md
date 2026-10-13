---
status: draft
system: platform
created: 2026-10-13
owners:
  - kandev
---

# Restart-stranded route recovery Requirements

## Overview

A backend restart can leave a dynamic-profile session with its route projected
at `starting` because the process-start callback that would carry the route to
`active` never arrived, an open restart-interrupted recovery block for the
turn, and an open turn row that no runtime will settle. Every recovery path then
refused one of the other two states, so the session could not make progress:
explicit history continuation was refused as `dynamic_route_pending`, and the
route action was refused as "route actions require a settled turn".

Platform owns this contract because it owns recovery of delivery state across
backend and shared-runtime restarts and the convergent disposition of the
recovery entry paths. The dynamic route state machine and its route actions are
owned by the
[dynamic agent routing design](../../agents/system-design/dynamic-agent-routing-02.md);
the turn and recovery-block persistence are owned by
[interrupted prompt recovery](../system-design/interrupted-prompt-recovery.md).
This requirement adds one guarded settlement path; it does not change route
selection, prompt admission, or recovery-block resolution.

## Terminology

- **Stranded starting route:** A persisted route whose status is `starting` with
  no live launch owner, because the launch that claimed it stopped or never
  reached its process-start callback.
- **Orphan turn:** An open turn row that no runtime can settle: no agent runs for
  the session and no resume, queued dispatch, or steer is in flight.
- **Restart-interrupted recovery block:** An open `session_recovery_blocks` row
  whose reason is one of `unknown_prompt_outcome`, `unresolved_durable_work`, or
  `unknown_queue_dispatch_outcome`. It records a turn that a restart left
  unsettled, so it proves the turn predates the current backend boot.
- **Parked route:** A dynamic route with no launch owner at `action_required`,
  `waiting`, `retry_wait`, or `waiting_for_reset`.

## Requirements

### REQ-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001: Settle a provably pre-boot orphan turn before a route action

**Intent:** A backend restart must not leave a dynamic-profile session where the
route action and the explicit recovery path each refuse the other's state,
without requiring manual database repair.

**User story:** As an operator, I want a restart-stranded dynamic session to
move again after restart, so that its saved conversation continues without
replacing a live execution or fabricating a terminal outcome.

#### Acceptance criteria

- **AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.1:** When a dynamic-profile
  session holds a `starting` route with no live launch owner, an open
  restart-interrupted recovery block for its turn, and an open orphan turn, the
  route action shall settle the orphan turn and proceed.
- **AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.2:** When the route is
  `starting` but any of these holds: no restart-interrupted recovery block, a
  live runtime for the session, the session is `RUNNING` or `STARTING`, or a
  resume, queued dispatch, or steer is in flight, the route action shall refuse
  and leave the route and turn unchanged.
- **AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.3:** A parked route shall
  retain its existing orphan-turn settlement behavior; this requirement does not
  change settlement for `action_required`, `waiting`, `retry_wait`, or
  `waiting_for_reset`.
- **AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.4:** Settlement shall not
  fabricate a terminal outcome, resend the interrupted prompt, clear unresolved
  work or route state, replace a live or surviving execution, or change the
  authenticated owner, route generation, cursors, active submission,
  replay/acknowledgement identity, queued messages, required input, or the
  original conversation.
- **AC-PLATFORM-RESTART-STRANDED-ROUTE-RECOVERY-001.5:** After the settlement
  described in AC-001.1, both the route action and explicit history continuation
  shall be admissible for the session; neither shall block the other.

## Out of scope

- New permission models, settings screens, recovery brokers, recurring
  checkers, or forced provider traffic.
- Blanket settlement of every open turn, or clearing unresolved work or route
  state to force progress.
- Changing route selection, dynamic-profile configuration, or prompt admission.
- Changing the way a recovery block is created or resolved.
- Office scheduler capacity, dispatch, or run admission, which are owned
  elsewhere.
