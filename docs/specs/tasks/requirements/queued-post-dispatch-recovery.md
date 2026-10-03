---
status: draft
system: tasks
created: 2026-10-01
owners:
  - kandev
---

# Queued Post-Dispatch Recovery Requirements

## Overview

A queued prompt can be accepted for one turn, then return a post-dispatch
error when Kandev terminates a stalled execution. The accepted queue row is
acknowledged so it is not replayed, but remaining rows can sit without a new
drain trigger until backend restart. The task system owns accepted queue-row
settlement, eligibility for the next queued row, and visible recovery when
the receiving session cannot continue.

## Requirements

### REQ-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001: Reconsider queued work after an accepted prompt fails

**Intent:** A terminal error after an accepted queued prompt must not silently
strand unrelated follow-up messages.

#### Acceptance criteria

- **AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.1:** When a queued prompt was
  accepted but post-dispatch handling fails, Kandev shall acknowledge that
  accepted row at most once and shall not send it as a new prompt again.
- **AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.2:** After the accepted turn
  settles, Kandev shall reconsider the next pending row through the normal
  Auto-run, session-identity, workflow-admission, clarification, and
  promptability guards without waiting for backend restart.
- **AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.3:** If the original session
  is terminal or has no usable runtime, Kandev shall use only an existing
  eligible recovery/recipient path. If none is available, it shall keep the
  remaining rows durable and expose that delivery is waiting for session
  recovery; it shall not claim that they ran or silently drop them.
- **AC-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001.4:** A concurrent completion,
  restart reconciliation, or later queue trigger shall not dispatch the next
  row twice or reorder it ahead of an older pending row. Auto-run OFF shall
  leave the queue pending.

## Compatibility and exclusions

This requirement does not change the prolonged-stall terminal classification,
create a fresh agent process solely to defeat a recovery gate, enable Auto-run,
increase queue capacity, or reorder an operator message. It complements
[queue admission](queue-admission.md) and the existing session recovery
contract.

## Implementation plans

- [Agent stream stall and queue recovery](../../../plans/agent-stream-stall-and-queue-recovery/plan.md)
