---
status: draft
system: tasks
created: 2026-10-01
owners:
  - kandev
---

# Peer Report Batch Delivery Requirements

## Overview

A controller can receive reports from several child tasks while it is busy. The
reports occupy separate queue rows because automatic merging preserves one
sender task per row. Delivering only one row after each turn lets a burst fill
the queue and delays the next operator instruction. The task system owns queue
order, delivery, sender identity, and recovery; the existing UI displays the
resulting queue and transcript.

## Terminology

- **Peer report:** An ordinary text-only queued agent message with a recorded
  sending task. Workflow, server, lifecycle, clarification, managed-input, and
  interrupt messages are not peer reports for this requirement.
- **Delivery batch:** A consecutive FIFO prefix of eligible peer reports
  delivered to one receiving session in one agent turn. Each report remains a
  distinct admitted message with its own sender and queue identity.

## Requirements

### REQ-TASKS-PEER-REPORT-BATCH-DELIVERY-001: Deliver consecutive peer reports together

**Intent:** Let a controller process a burst of child reports without spending
one turn per sender while preserving each report's source and order.

#### Acceptance criteria

- **AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.1:** When Auto-run is enabled and
  the FIFO head is a peer report, the next eligible turn shall deliver a
  bounded consecutive prefix of compatible peer reports in one agent turn.
  Reports from different child tasks may share that turn.
- **AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.2:** The delivered input shall keep
  each report's original body in FIFO order and identify its sending task for
  the receiving agent. The transcript shall retain a distinct message and
  sender attribution for every report in the batch.
- **AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.3:** The batch shall stop before
  the first user, workflow, server, lifecycle, clarification, managed-input, or
  otherwise incompatible entry. That entry and later entries shall keep their
  FIFO order. A single eligible report shall still be delivered normally.
- **AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.4:** A batch shall contain at most
  ten reports and at most 64 KiB of combined report bodies. If adding the next
  report would exceed either bound, that report shall remain queued. An
  oversized head shall still be deliverable alone without truncation.
- **AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.5:** Batch reservation and
  acknowledgement shall be scoped to the exact task, session, and session
  incarnation. Concurrent drains shall not deliver a report twice, and a
  failed or interrupted dispatch shall preserve every unaccepted report in
  its original order for retry.
- **AC-TASKS-PEER-REPORT-BATCH-DELIVERY-001.6:** Auto-run OFF, an active
  clarification, a workflow admission barrier, and an active queue edit shall
  retain their existing delivery barriers. An in-flight reservation may be
  omitted from the pending count under the existing queue status contract;
  a failed attempt shall restore its reports to pending status rather than
  present them as successfully delivered.

## Compatibility and exclusions

Admission-time Auto-merge remains limited by its existing same-sender rules.
Manual merge, queue capacity, user submissions, Send now, attachments, entity
references, passthrough delivery, and special lifecycle prompts retain their
current contracts. This requirement does not change the queue cap or guarantee
admission while a receiving agent is still in a long-running turn. It improves
the rate at which an eligible controller clears a burst when a turn becomes
available. No new queue setting or UI control is required.

## Related contracts

- [Queue admission](queue-admission.md) owns acceptance and the capacity error.
- [Automatic merge overrides](../../ui/requirements/message-queue-auto-merge-session-overrides.md)
  own admission-time folding.
- [Queue management](../../ui/requirements/message-queue-management.md)
  owns the displayed queue and cap controls.

## Implementation plans

- [Peer report batch delivery](../../../plans/peer-report-batch-delivery/plan.md)
