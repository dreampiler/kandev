---
created: 2026-10-29
status: in_progress
requirements:
  - REQ-TASKS-QUEUED-SESSION-OWNERSHIP-004
system_design:
  - ../../specs/tasks/system-design/queued-session-ownership.md
legacy_specs: []
---

# Implementation plan: Explicit deferred-launch discard

## Overview

A deferred workflow launch can sit queued for hours. Until now the only way to
remove one was to let the automatic replay sweep decide it was no longer
replayable, which is a system judgment, not a user decision. This package adds
one explicit, task-scoped discard for a launch that is still valid, and answers
with what actually happened to the durable record.

The [requirements](../../specs/tasks/requirements/queued-session-ownership.md)
and [design](../../specs/tasks/system-design/queued-session-ownership.md) stay
owned by the queued session ownership pair: the deferred record and its
recipient belong to the task system, and agent capacity keeps owning admission.
Read the "Deferred entry ownership" and "Explicit discard" design sections.

Work order: [task 01](task-01-explicit-deferred-launch-discard.md).

## Contract status

The transport is a proposal, not an agreed contract. The issue asking upstream
about the shape (`kdlbs/kandev#4508`) has no maintainer answer, so this package
implements the recommended safe shape and keeps it replaceable:

- method and path: `POST /api/v1/tasks/:id/deferred-launch/discard`
- response: `200 {"outcome": "already_clear" | "discarded" | "retained", "reason"?: ...}`
- authorization: the existing task-write and executor-mutation checks; no new
  global operator permission is introduced

A maintainer who prefers a different verb, path, field set, or an operator-only
permission can change the thin transport without touching the durable safety
rules, because those live in the shared settlement routine rather than in the
handler.

## Scope

### In scope

- One explicit discard entry point for a pending ceiling-deferred launch.
- A closed three-way outcome plus a closed set of retained reasons, so an
  incomplete discard is never reported as a no-op or a success.
- One settlement routine shared by the automatic replay drop and the explicit
  path: settle the served automation run, clear only the ceiling keys under the
  entry-admission lock with an identity CAS, release automation start ownership,
  publish the task update. A failure stops before ownership release.
- A live dispatch claim and a successor record are never cleared.
- Task-write plus executor-mutation authorization, reused from task mutation.
- Focused backend tests in the existing ceiling replay test file.

### Out of scope

- Starting, resuming, stopping, or steering any session; performing a manual
  launch; changing task state directly.
- New permissions, roles, database schema, configuration keys, metrics, or a
  second queue.
- Frontend surfaces. The existing queue projection already disappears when the
  ceiling record is cleared; a discard affordance is a separate UI decision.
- Changing the automatic replay drop's own reasons or timing.

## Delivery order

One work order. The durable service and its tests come first in the same change
as the transport, because a transport without a proven settlement path would be
the exact "bool only" shortcut this package replaces.

## Evidence and conformance

The safety properties are pinned by tests colocated with the existing ceiling
replay tests: no-op, success with a coexisting non-ceiling key, settlement
failure, clear failure, live claim, successor record, unreadable record, and a
running session left untouched. The route registration has its own assertion.
The transport's HTTP authorization path reuses the same helpers as the deferred
prompt edit.

## Boundaries

No production deployment, restart, production database write, or live endpoint
call. No public issue, comment, push, or pull request from this package. No new
permission, role, schema, secret, or lock bypass, and no change to model routing,
work-order order, or limits.
