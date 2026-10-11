---
status: draft
system: office
requirements:
  - REQ-OFFICE-ERROR-CLEAR-001
  - REQ-OFFICE-ERROR-CLEAR-002
---

# Office: Agent Error Display and Dismissal System Design

## Purpose and boundaries

This design adds one dismissal kind and one read-time clearing rule to the
Office inbox. It changes the computed inbox view and the per-agent failure
badge; it adds no table, no column, no migration, and no background job. The
dismissal store (`office_inbox_dismissals`) and its per-user semantics already
exist.

It does not own failure detection or retry (`apps/backend/internal/office/service/failure.go`,
`retry.go`), auto-pause, run-history retention, or the clarification "Needs you"
inbox. Those keep their current contracts. The Office inbox stays a computed
view over its sources; nothing here turns it into a stored table.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-OFFICE-ERROR-CLEAR-001` | [Dismissal](#dismissal), [Control flow](#control-flow), [Persistence](#persistence), [Failure and recovery](#failure-and-recovery) |
| `REQ-OFFICE-ERROR-CLEAR-002` | [The unresolved-failure criterion](#the-unresolved-failure-criterion), [Consistent counting](#consistent-counting), [Frontend](#frontend) |

## Components and responsibilities

- `apps/backend/internal/office/dashboard/handler_inbox.go`'s
  `dismissInboxItem` accepts the new kind and routes it to the service.
- `apps/backend/internal/office/service/failure.go`'s `Service` gains
  `MarkAgentErrorFixed`, which verifies the stored identity and records the
  dismissal. It mirrors `MarkAgentRunFailedFixed` without requeueing anything:
  an `agent.error` entry names an already-terminal run, so there is no run to
  resume (`AC-OFFICE-ERROR-CLEAR-001.4`).
- `apps/backend/internal/office/dashboard/service_inbox.go`'s
  `inboxAgentErrorItems` gains the dismissal filter and the unresolved-failure
  filter, and enriches the item payload so the frontend can attribute it to an
  agent. `GetInboxCount` stops reading raw `agent.error` activity and counts the
  same computed items.
- `apps/backend/internal/office/repository/sqlite/` gains one read that answers
  "is there a successful run in this scope newer than this instant?" (or the
  set form of it) plus a read of a single activity entry by id.
- `apps/web/app/office/inbox/inbox-item-row.tsx` renders the dismiss control for
  `agent_error`; `inbox-page-client.tsx` includes it where eligible;
  `apps/web/components/app-sidebar/sections/agents-section.tsx` counts every
  failure surface for the agent from the same item set.

## The unresolved-failure criterion

For a failure occurrence `F` with occurrence instant `tF` in an agent and task
scope `S`:

```text
resolved(F)  =  exists a successful run R in S with finished_at(R) > tF
unresolved(F) = not dismissed(F) and not resolved(F)
```

Established conservatively per `AC-OFFICE-ERROR-CLEAR-002.4`: `resolved` is
true only when a success strictly newer than `tF` is read. A missing or
unreadable success leaves `resolved` false, so the failure stays visible. This
is deliberate rather than a fallback: hiding a possibly-current failure is worse
than showing a stale one, and the operator still has explicit dismissal.

Task scope `S` is the run's `task_id` when the payload names one, and the
taskless agent scope otherwise (`AC-OFFICE-ERROR-CLEAR-002.7`). Matching on the
pair, not the agent alone, keeps a success on task A from clearing a failure on
task B.

The occurrence instant of a failure occurrence is:

- a failed `runs` row: `COALESCE(finished_at, requested_at)`. This reuses the
  value `ListFailedRunsForInbox` already orders by.
- an `agent.error` activity entry: the entry's `created_at`, which the scheduler
  writes at the same escalation that marked the run failed.

The success instant is `finished_at` of a `runs` row with status `finished`
(the only terminal success status; `runs/models.RunStatusFinished`). A success
whose `finished_at` is unparseable is not newer than any failure, so it clears
nothing (`AC-OFFICE-ERROR-CLEAR-002.4`).

## Consistent counting

`AC-OFFICE-ERROR-CLEAR-002.5` is satisfied by deriving both surfaces from the
computed inbox items:

- `GetInboxItems` already assembles `agent_run_failed` and `agent_error` items
  from the same sources. Applying the criterion inside each item builder means
  the item list and the count cannot disagree.
- `GetInboxCount` currently counts pending approvals, budget activity, raw
  `agent.error` activity, permissions, and review requests independently. The
  raw `agent.error` term is replaced by the count of the computed
  failure items (both kinds) so dismissal and last-success clearing move the
  badge exactly as they move the list.
- The sidebar agent badge is a client-side reduction over the same inbox items.
  It counts both `agent_run_failed` and `agent_error` items whose agent matches
  the row. Because the server already filtered them, the badge inherits the
  criterion without a second server query.

Attribution uses the item payload. `agent_run_failed` already carries
`agent_profile_id`; `agent_error` gains the same fields (`agent_profile_id`,
`run_id`, `task_id`) so the badge can match it the same way.

## Control flow

Dismissal (`REQ-OFFICE-ERROR-CLEAR-001`):

1. The inbox returns an `agent_error` item whose `id` is the activity entry id,
   whose `entity_id` is the agent profile id, and whose payload names the run.
2. The user activates the row's dismiss control; the client posts
   `{kind: "agent_error", item_id: <activity entry id>}` to the existing inbox
   dismiss route.
3. `dismissInboxItem` validates a non-empty kind and id and switches on the
   kind, adding an `agent_error` case that calls `MarkAgentErrorFixed`.
4. `MarkAgentErrorFixed` reads the activity entry by id, confirms it is an
   `agent.error` entry, and records the dismissal under
   `(kind=agent_error, item_id=<id>)` via the existing
   `DismissInboxItem` write.
5. The next inbox fetch filters the row out.

Clearing by success (`REQ-OFFICE-ERROR-CLEAR-002`):

1. `GetInboxItems` builds each failure item.
2. For `agent_run_failed`, the failed-run query keeps its dismissal exclusion
   and additionally excludes a run for which a newer success exists in the same
   scope.
3. For `agent_error`, each candidate entry is matched against the same success
   evidence using its `run_id` to resolve the agent and task scope.
4. Surviving items are returned and counted.

## Failure and recovery

- **Unreadable success evidence.** `AC-OFFICE-ERROR-CLEAR-002.4`: any error
  resolving a later success leaves the failure unresolved. The item builder logs
  a warning and keeps the row visible rather than failing the whole fetch, so a
  broken read degrades to "still shown" rather than "silently hidden".
- **Dismissing an identity that no longer exists.** `AC-OFFICE-ERROR-CLEAR-001.3`:
  `MarkAgentErrorFixed` reports that nothing was dismissed instead of writing a
  dismissal keyed to an unrelated id. The handler returns the corresponding
  outcome; the client refetches.
- **A failure with no parseable instant.** It is not older than any success, so
  it stays unresolved. This matches the existing "unparseable timestamp is zero
  time" handling and keeps the conservative direction.
- **A success arriving between read and write.** Clearing is computed per read,
  so the next fetch reflects it; no reconciliation is required and no state is
  cached (`AC-OFFICE-ERROR-CLEAR-002.3`).

## Persistence

- No schema change. Dismissals persist in `office_inbox_dismissals` exactly as
  today, keyed by `(user_id, item_kind, item_id)` with `item_kind='agent_error'`.
- No source row is mutated or deleted (`AC-OFFICE-ERROR-CLEAR-002.3`). A
  dismissed or success-cleared failure remains in `runs` and
  `office_activity_log`.
- The inbox stays a computed view; the badge count is recomputed per fetch.
  Restart changes nothing: identical inputs produce identical output.

## Security

- The dismiss route is workspace-scoped by the existing Office route middleware;
  `agent_error` adds no new route and no new param. No new authorization path is
  introduced.
- The dismissal remains per user; no cross-user visibility change
  (`AC-OFFICE-ERROR-CLEAR-001.5`).

## Observability

No new metric, counter, or feature toggle. This is a display and dismissal
correctness fix on an existing surface. The existing `agent.error` activity
entry remains the durable audit record; a dismissal adds only a display-layer
row. The work order records the focused unit evidence instead.

## Frontend

- `inbox-item-row.tsx` extends the dismiss-control condition and the
  `MarkFixedButton` guard to include `agent_error`, reusing the existing
  translated labels (`office:markFixed`, `office:marking`) and the existing
  `office:agentError` type label. No new English literal is introduced.
- `inbox-page-client.tsx`'s "mine" filter includes `agent_error` alongside the
  other actionable failure kinds so the row is reachable where dismissals
  happen.
- `agents-section.tsx` counts `agent_run_failed` and `agent_error` items for the
  row and keeps the existing `sidebar:runErrors` plural key.
- Any new user-facing string is added through `t()` / `<Trans>` to every
  supported locale catalog; no hardcoded literal. `pnpm run i18n:check` is the
  gate.
- The dismiss control is an ordinary button at the surrounding density on fine
  pointers and at the touch target size on coarse pointers, matching the
  existing Mark-fixed control already in this row. Because the row exists on the
  desktop and the phone layout, the phone composition is verified with the
  existing responsive inbox layout; no new navigation is added.
- ASCII UI preview is in the plan and the work order.

## Test plan

Backend, table-driven, beside the changed logic:

- `handler_inbox.go`: `agent_error` is accepted and routed; an unknown kind is
  still rejected; an empty kind or id is rejected (`001.1`).
- `MarkAgentErrorFixed`: records a dismissal for a stored `agent.error` id;
  reports no dismissal for an unmatched id and does not write a dismissal for a
  different entry (`001.2`, `001.3`).
- Inbox builder: a dismissed `agent_error` is absent from items and count, and
  the activity row is unchanged (`001.4`).
- Unresolved criterion: a failure older than a later same-scope success is
  suppressed; a failure newer than the latest success remains; an equal-instant
  success does not clear; a taskless failure is scoped to the agent
  (`002.1`, `002.2`, `002.7`).
- A read error resolving success evidence keeps the failure visible (`002.4`).
- Count consistency: the count equals the number of computed failure items
  including `agent_error`, and dismissal/last-success moves both together
  (`002.5`).
- Auto-paused agents still produce their entry and are not suppressed
  (`002.6`).

Frontend, Vitest:

- `inbox-item-row` renders the dismiss control for `agent_error` and calls the
  dismiss client with `(agent_error, id)`.
- `agents-section` counts both failure kinds for the matching agent and clears
  when the item is removed.

No E2E or permanent integration test is added: the change is proven at the
existing unit boundaries and the real API/card readback is a temporary isolated
reproduction, per the execution contract.

## Related decisions

None. This is a display-and-dismissal correctness change on an existing
computed view, with no durable boundary, contract, or ownership change that
requires an ADR.
