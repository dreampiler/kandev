---
status: draft
system: tasks
created: 2026-10-01
owners:
  - task-lifecycle
---

# Child-turn Stalled Signal Requirements

## Overview

A parent needs an attributed notification when a child finishes a live turn
in an actionable stalled segment. Ordinary turns must not consume the serial
parent's agent capacity. Tasks owns this contract because child relationships,
turn identity, workflow entry, and parent-session delivery are task primitives;
Office terminal-child waves remain a separate contract.

This is an unaccepted design package. Option B is recommended, not selected by
the owner. Implementation requires that decision and a later implementation
instruction. See the [plan](../../../plans/child-turn-stalled-signal/plan.md).

## Terms

- **Live settlement:** a previously executing turn has ended, excluding orphan
  abandonment, rejected dispatch, and cleanup-only closure.
- **Workflow entry:** one transition generation, not merely a step name. Leaving
  and returning to the same step creates a different entry.
- **Candidate:** a settlement awaiting eligibility checks and classification.
- **Alert:** one classified item queued for the current parent's primary session.

## Requirements

### REQ-TASKS-CHILD-STALL-001: Eligible stalled settlements

**Intent:** Surface actionable child stalls without equating turn end with
workflow completion.

- **AC-TASKS-CHILD-STALL-001.1:** A live settlement shall qualify only while the
  child is non-archived and non-terminal, retains its captured parent and
  workspace, has no active execution or turn in the settling session, and
  retains the workflow entry captured at turn start.
- **AC-TASKS-CHILD-STALL-001.2:** An unresolved question/input, execution
  error/quota, or absence of a completion signal required for that turn shall
  be actionable. Successful ordinary turns, optional signal absence, explicit
  cancellation, and abandonment shall not alert. Unknown evidence shall not
  be classified as failure or success.
- **AC-TASKS-CHILD-STALL-001.3:** Eligibility shall be checked again immediately
  before parent delivery. A committed deferred transition, same-step re-entry,
  reparenting, archive, terminal state, or resumed execution shall prevent stale
  delivery. Failed reads shall defer delivery without workflow mutation.

### REQ-TASKS-CHILD-STALL-002: Durable selective delivery

**Intent:** Preserve actionable alerts across failures without duplicate items
or unauthorized wakes.

- **AC-TASKS-CHILD-STALL-002.1:** Repeated or reordered observations and retries
  of the same child/session/turn/start-entry identity shall create at most one
  parent item, including crashes between queue insertion and acknowledgement.
- **AC-TASKS-CHILD-STALL-002.2:** An eligible alert shall use only its parent's
  primary session in the same workspace. An offline or busy parent shall retain
  a queued item under existing admission and Auto-run rules, without interruption,
  forced session creation, queue reordering, or bypass of pending questions.
- **AC-TASKS-CHILD-STALL-002.3:** Lost event delivery and consumer restart shall
  leave candidates recoverable. Transient delivery failures shall retry with
  bounded backoff; exhausted retries shall remain visibly failed and available
  for operator retry using the original identity.
- **AC-TASKS-CHILD-STALL-002.4:** Ingestion, classification, retry, and notification
  shall not move either task's workflow, complete a task, answer a question,
  cancel a session, or authorize a new agent action independently of the existing
  queue dispatch policy.

### REQ-TASKS-CHILD-STALL-003: Attribution and operating visibility

**Intent:** Make the alert and its delivery state reviewable.

- **AC-TASKS-CHILD-STALL-003.1:** The parent history shall show an attributed
  system item containing the child link, workflow step, cause category, and
  turn reference. The same information shall be readable and the child link
  operable on desktop and phone, without a hidden prompt-only delivery.
- **AC-TASKS-CHILD-STALL-003.2:** Operators shall be able to inspect queued,
  delivered, failed, and suppressed outcomes plus monitor lag and bounded error
  reasons, without reading credentials or raw message bodies in the event feed.
- **AC-TASKS-CHILD-STALL-003.3:** Candidate storage shall use compact metadata
  with bounded resolved retention, not a new permanent per-turn payload archive.
  Cleanup shall not permit replay to recreate an already resolved parent item.

## Exclusions

No implementation, permanent tests, deployment, settings changes, public
webhook, new autonomous recovery policy, transcript scanning model, parent
priority lane, or replacement of Office all-children-terminal wake receipts is
authorized by this package. Traffic observations are sizing input, not runtime
thresholds. Existing question and task-completion contracts remain authoritative.
