---
status: draft
system: office
created: 2026-10-11
owners:
  - kandev
---

# Office: Agent Error Display and Dismissal Requirements

## Overview

The Office inbox is a computed view over several sources, two of which report
agent failures: `agent_run_failed` (one row per failed run) and `agent_error`
(one row per `agent.error` activity entry written when a run fails permanently
after exhausting its retries). Only `agent_run_failed` and
`agent_paused_after_failures` have a dismissal path. An `agent_error` row has
none, so a failure an operator has already resolved stays on screen; an attempt
to dismiss it through the shared inbox action is rejected as an unsupported
kind.

The two failure surfaces are also scored by different rules. The inbox item list
reports `agent_error` rows, but the per-agent badge in the sidebar counts only
`agent_run_failed` rows, and the inbox badge count reads raw `agent.error`
activity independently of dismissal. Nothing reconciles a failure that predates a
much later success for the same agent and task, so a stale error is displayed
indefinitely even after many successful runs.

This document owns the smallest consistent contract for clearing a stale error:
an explicit, durable dismissal for `agent_error`, and a read-time
last-success criterion shared by every failure surface. It does not own failure
detection, retry, or auto-pause, which stay with their existing requirements.
It is separate from delivery retention, restart admission, and permission
locks.

## Terminology

- **Failure occurrence:** a `runs` row with status `failed`, or the
  `agent.error` activity entry produced for such a run.
- **Failure surface:** either inbox source that reports a failure occurrence:
  `agent_run_failed` or `agent_error`.
- **Successful run:** a `runs` row with status `finished`.
- **Unresolved failure:** a failure occurrence that is newer than the latest
  successful run for the same agent and task scope, or for which a later success
  cannot be established, and that has not been dismissed.
- **Dismissal:** a per-user record in `office_inbox_dismissals`, keyed by
  `(kind, item_id)`, that hides an item without altering its source row.

## Requirements

### REQ-OFFICE-ERROR-CLEAR-001: An agent error can be explicitly dismissed

**Intent:** An operator who has resolved an Office agent error must be able to
clear it from the inbox without deleting the audit history. Today only two
kinds are accepted, so an `agent_error` row cannot be dismissed at all.

**User story:** As an Office operator, I want to dismiss an agent error the same
way I dismiss a failed run, so that an error I have fixed stops demanding
attention without losing its record.

#### Acceptance criteria

- **AC-OFFICE-ERROR-CLEAR-001.1:** The inbox dismiss action shall accept the
  `agent_error` item kind in addition to `agent_run_failed` and
  `agent_paused_after_failures`. A request for `agent_error` shall not be
  rejected as an unsupported kind.
- **AC-OFFICE-ERROR-CLEAR-001.2:** The dismissal shall be recorded against the
  same item identity the inbox returned for that row: the `office_activity_log`
  identifier of the `agent.error` entry. It shall not be re-derived from the
  agent or the run at dismissal time.
- **AC-OFFICE-ERROR-CLEAR-001.3:** The dismissal shall be recorded only when
  the named identity matches a stored `agent.error` entry. A request whose
  identity cannot be matched shall not clear a different entry, and shall
  report its own outcome so a caller can tell "dismissed" from "nothing was
  dismissed".
- **AC-OFFICE-ERROR-CLEAR-001.4:** A dismissed `agent_error` row shall be
  absent from both the inbox item list and the inbox count on every subsequent
  read, and the underlying activity row shall remain unchanged. History is
  retained; only the display is suppressed.
- **AC-OFFICE-ERROR-CLEAR-001.5:** Dismissal shall keep the existing per-user
  semantics of `office_inbox_dismissals`, including the shared auto-dismiss
  sentinel, so a dismissal written for one user does not hide the row from
  another unless written under that sentinel.

### REQ-OFFICE-ERROR-CLEAR-002: A later success clears a stale failure under one shared criterion

**Intent:** A failure that predates a much later success for the same agent and
task is stale and should stop demanding attention, while a newer failure must
never be hidden by an older success. Every surface that counts failures must
score them the same way, or the sidebar badge and the inbox disagree.

**User story:** As an Office operator, I want a failure that later runs already
resolved to stop being counted, so that the error badges reflect the agent's
current health rather than its history.

#### Acceptance criteria

- **AC-OFFICE-ERROR-CLEAR-002.1:** The system shall define an unresolved
  failure for an agent and task scope as a failure occurrence whose occurrence
  instant is newer than the latest successful run instant in the same scope, or
  a failure for which no later successful run can be established. The
  criterion shall be identical for every failure surface.
- **AC-OFFICE-ERROR-CLEAR-002.2:** A successful run shall clear only a failure
  it is newer than. A failure whose occurrence instant is at or after the
  latest successful run, or that has no prior success, shall remain unresolved.
  An older success shall never clear a newer failure.
- **AC-OFFICE-ERROR-CLEAR-002.3:** Clearing shall be a read-time suppression.
  The failed `runs` row and the `agent.error` activity row shall not be deleted,
  rewritten, or marked resolved. The failure remains in history and in the
  activity log.
- **AC-OFFICE-ERROR-CLEAR-002.4:** When the evidence needed to establish a later
  success cannot be read, the failure shall remain visible. The clearing rule
  shall fail conservatively and shall never hide a failure on an unreadable or
  ambiguous read.
- **AC-OFFICE-ERROR-CLEAR-002.5:** The per-agent failure count shown on an
  agent card or row and the workspace inbox item list and count shall be derived
  from the same unresolved-failure set. A failure counted by one shall be
  counted by the other for the same agent, and a failure cleared from one shall
  be cleared from the other.
- **AC-OFFICE-ERROR-CLEAR-002.6:** An agent whose consecutive-failure counter
  has tripped auto-pause shall continue to be reported by the existing
  auto-paused surface. The criteria here govern the failure rows, not the pause
  state, and shall not suppress an auto-paused agent entry.
- **AC-OFFICE-ERROR-CLEAR-002.7:** A taskless failure occurrence (one whose run
  carries no task) shall be scored in its own agent scope, so a successful run
  for a different task shall neither clear it nor be cleared by it.

## Out of scope

- **Failure detection, retry policy, and auto-pause thresholds.** Owned by the
  existing Office runtime and scheduler requirements; unchanged here.
- **Retention or deletion of runs and activity rows.** Run-history retention is
  a separate capability. This document only changes what is displayed.
- **Restart admission, delivery retention, and permission locks.** Explicitly
  separate, per the originating request.
- **A new notification, approval, or escalation surface.** No new inbox kind,
  no notification event, no wakeup.
- **Operator tunability of the criterion.** The last-success rule is fixed
  behavior, not a setting.
- **The `agent_paused_after_failures` lifecycle.** Its dismissal already
  exists; its unpause recovery is unchanged.
- **Suppressing a still-relevant failure for history.** History rows are never
  removed; only the computed inbox view is filtered.
