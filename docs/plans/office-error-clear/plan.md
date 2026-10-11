---
created: 2026-10-11
status: draft
requirements:
  - REQ-OFFICE-ERROR-CLEAR-001
  - REQ-OFFICE-ERROR-CLEAR-002
system_design:
  - ../../specs/office/system-design/office-error-clear-01.md
legacy_specs: []
---

# Implementation Plan: Office agent error display and dismissal

Clear a stale Office agent error from the computed inbox view either by an
explicit, durable dismissal or by a later successful run in the same agent and
task scope, and make the per-agent failure badge and the inbox count use the
same unresolved-failure criterion. Stop rejecting `agent_error` dismissal as an
unsupported kind. Do not delete history, and do not let an older success clear a
newer failure.

This is one vertical slice: the same contract reaches the backend inbox
computation, the dismiss route, the inbox UI, and the sidebar badge. It is
separate from delivery retention, restart admission, and permission locks.

## Work orders

- [Task 01: Consistent clear and dismiss for Office agent errors](task-01-office-error-clear.md)

## Scope

In scope:

- `agent_error` as an accepted inbox dismissal kind, keyed on the stored
  activity identity, failing conservatively on an unmatched identity.
- A read-time last-success rule shared by `agent_run_failed` and `agent_error`.
- Counting the sidebar agent badge and the inbox count from the same computed
  items.
- The dismiss control and "mine" eligibility for `agent_error` on desktop and
  phone, all copy through `t()`/`<Trans>` in every supported locale.

Out of scope: failure detection, retry, auto-pause thresholds, run-history
retention, restart admission, delivery retention, permission locks, any new
notification or approval surface, and any operator setting.

## UI-01: Office inbox failure row (dismiss control)

Entry point: `/office/inbox`, "Mine" tab, an `agent_error` row. State: active
(unresolved). Current behavior on the left, proposed on the right.

```text
UI-01 current (agent_error)              UI-01 proposed (agent_error)
+--------------------------------+       +--------------------------------+
| [bug] Agent error   active     |       | [bug] Agent error   active     |
|       <details>                |       |       <details>                |
|                                |       |           [ Mark fixed ]       |
+--------------------------------+       +--------------------------------+
```

Structural requirements: the dismiss control reuses the existing Mark-fixed
button and translated labels already in this row, sits after the row body, and
participates in the same touch sizing as the existing Mark-fixed control. Phone
composition is the existing responsive inbox list; the control stays in the row
and reaches the coarse-pointer touch target. Illustrative only: exact icon,
spacing, and column widths.

Maps to `AC-OFFICE-ERROR-CLEAR-001.1` (kind accepted), `.4` (row leaves list and
count). The full preview with the sidebar badge is in the work order.

## Verification

The work order owns the focused backend unit tests beside the changed
`dashboard`/`service` code, the frontend Vitest tests for the row and the badge,
and the repo lint commands. A temporary isolated real API and card readback
lives in an untracked location outside automatic test collection; no permanent
integration or E2E test is added.
