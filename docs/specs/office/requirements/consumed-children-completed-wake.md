---
status: active
system: office
created: 2026-10-14
owners:
  - kandev
---

# Office Consumed Children-Completed Wake Requirements

## Overview

A `task_children_completed` wake can wait in the queue far longer than the work
it announces. For example, a run parked behind the session ceiling can wait for
hours and then find its parent already finished. Launching it at that point
starts an agent turn with nothing left to coordinate.

This capability is consumer-side. It judges the queued run at launch time. It
does not change any producer's readiness or terminality predicate, and it does
not change how wakes are identified, deduplicated, or coalesced. Those rules
belong to [parent wake wave identity](parent-wake-wave-identity.md).

## Terminology

- **Consumed wake:** A queued `task_children_completed` run whose parent task is
  already `COMPLETED` or `CANCELLED` when the run reaches launch.
- **Periodic wake:** A wakeup request whose effective reason (`reason`, else
  `source`) is `routine_dispatch_cron` or the retired `heartbeat`.

## Requirements

### REQ-OFFICE-CONSUMED-WAKE-001: A consumed wake does not launch an empty turn

**Intent:** Spend no agent turn on a wake the parent's own completion has
already answered, and never drop a wake that still carries something live.

#### Acceptance criteria

- **AC-OFFICE-CONSUMED-WAKE-001.1:** The run shall be cancelled without starting
  an agent, with cancel reason `parent_task_terminal`, when all of the following
  hold:
  - the check runs on a processing pass before launch, including the pass that
    picks up a run lifted from a session-capacity park;
  - the run's reason is `task_children_completed` or the legacy
    `children_completed`;
  - an authoritative read shows the parent task named by the run payload's
    `task_id` is `COMPLETED` or `CANCELLED`.

  This is the parent terminal predicate the backstop candidate query already
  applies.
- **AC-OFFICE-CONSUMED-WAKE-001.2:** If the state read fails, the run shall follow
  the existing staleness read-failure path, which retries rather than cancels. A
  run whose parent is not terminal, or whose payload names no task, shall be
  unaffected.
- **AC-OFFICE-CONSUMED-WAKE-001.3:** A run that has any non-periodic wakeup
  request coalesced into it shall launch as before, so the event-triggered wake
  it carries is never dropped. Periodic wakes coalesced into a cancelled run
  shall be released the way an idle skip releases them.
- **AC-OFFICE-CONSUMED-WAKE-001.4:** The cancelled run shall keep its row, wave
  columns, and causation. It shall still count as a terminal run for
  `AC-OFFICE-WAKE-WAVE-IDENTITY-003.1`. No other reason, producer, idempotency
  key, or coalescing rule shall change.

## Out of scope

- Deciding when a parent's children are terminal enough to wake it
  (`AC-OFFICE-WAKE-WAVE-IDENTITY-004.7`).
- Session-ceiling admission order for coordinator launches.
- Reasons other than children-completed.
