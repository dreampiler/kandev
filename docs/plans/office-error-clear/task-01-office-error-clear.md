---
id: task-01
title: Consistent clear and dismiss for Office agent errors
status: pending
wave: 1
depends_on: []
plan: docs/plans/office-error-clear/plan.md
---

# Consistent clear and dismiss for Office agent errors

## Title

Implement `agent_error` dismissal and a shared last-success clearing rule so the
per-agent failure badge and the inbox count agree, without deleting history.

## Inputs

- Requirement `docs/specs/office/requirements/office-error-clear.md`
  (`REQ-OFFICE-ERROR-CLEAR-001`, `REQ-OFFICE-ERROR-CLEAR-002` and their ACs).
- System design `docs/specs/office/system-design/office-error-clear-01.md`,
  including its Test plan.
- Scoped guidance `apps/backend/internal/office/AGENTS.md` and
  `apps/web/AGENTS.md`.
- Existing shapes to reuse: `handler_inbox.go`'s dismiss switch,
  `service/failure.go`'s `MarkAgentRunFailedFixed`, `dashboard/service_inbox.go`'s
  `inboxAgentRunFailedItems` / `inboxAgentErrorItems` / `GetInboxCount`,
  `repository/sqlite/failure.go`'s `ListFailedRunsForInbox`,
  `DismissInboxItem`, and `ListActivityEntriesByAction`, `runs/models`
  `RunStatusFinished`, and the frontend `inbox-item-row.tsx` /
  `agents-section.tsx` surfaces.
- `D:\...\p200_index\설계카드.md` invariant 3 (user-visible control of periodic
  and automatic operation) is the design basis; no invariant conflict.

## Acceptance

1. `agent_error` is an accepted dismiss kind; `MarkAgentErrorFixed` records a
   dismissal against the stored `agent.error` activity id, reports no dismissal
   for an unmatched id without clearing a different entry, and never deletes the
   activity row.
2. `GetInboxItems` and `GetInboxCount` apply the same unresolved-failure rule to
   `agent_run_failed` and `agent_error`: a failure older than a later successful
   run in the same agent and task scope is suppressed, a failure at or newer
   than the latest success remains, a taskless failure is scoped to the agent,
   and an unreadable success read keeps the failure visible.
3. The inbox item row offers the dismiss control for `agent_error`, the "mine"
   filter includes it, and the sidebar agent badge counts both failure kinds for
   the row from the same item set; all copy is translated in every supported
   locale and the row keeps its touch sizing on phone.

## In scope

- `dismissInboxItem` routing and `MarkAgentErrorFixed`.
- The `agent_error` payload enrichment needed for per-agent attribution.
- The unresolved-failure read (success evidence per agent and task scope).
- `GetInboxCount` counting the computed failure items.
- The frontend row, "mine" filter, and sidebar badge, with i18n and phone
  layout parity.

## Out of scope

- Failure detection, retry, auto-pause thresholds, and the
  `agent_paused_after_failures` lifecycle.
- Run-history retention, restart admission, delivery retention, permission
  locks.
- Any new notification, approval, escalation, metric, or feature toggle.
- Any schema change or migration.
- Any change to the clarification "Needs you" inbox.

## UI-02: Sidebar agent row badge (consistency)

Entry point: the sidebar Agents section with an agent that has an unresolved
`agent_error`. Before, the badge ignores `agent_error`; after, both failure kinds
count.

```text
UI-02 current                           UI-02 proposed
+-------------------------------+       +-------------------------------+
| [A] coordinator     1 error   |       | [A] coordinator     2 errors  |
+-------------------------------+       +-------------------------------+
```

Structural requirement: the badge is the count of unresolved failure items whose
agent matches the row. Illustrative only: exact width and badge shape. Phone
composition is the existing sidebar sheet; the badge node is unchanged in shape.
Maps to `AC-OFFICE-ERROR-CLEAR-002.5`.

## Files

- `apps/backend/internal/office/dashboard/handler_inbox.go` (+ test)
- `apps/backend/internal/office/service/failure.go` (+ test)
- `apps/backend/internal/office/dashboard/service_inbox.go` (+ test)
- `apps/backend/internal/office/repository/sqlite/failure.go` or `activity.go`
  (+ test) for the success-evidence read
- `apps/web/app/office/inbox/inbox-item-row.tsx` (+ test)
- `apps/web/app/office/inbox/inbox-page-client.tsx` (if the filter is extracted
  for testability)
- `apps/web/components/app-sidebar/sections/agents-section.tsx` (+ test)
- locale catalogs only if a genuinely new key is required

## Verification

- `cd apps/backend && go test -trimpath ./internal/office/dashboard/... ./internal/office/service/... ./internal/office/repository/sqlite/...`
- `cd apps/backend && make lint`
- `cd apps && pnpm --filter @kandev/web typecheck`
- `cd apps/web && pnpm exec vitest run app/office/inbox components/app-sidebar/sections/agents-section.test.tsx`
- `cd apps/web && pnpm run i18n:check`
- Temporary isolated real API/card dismissal and count readback in an untracked
  location outside automatic collection; no operating notification write, no
  operating DB or process mutation.

## Results

Implemented and verified. PR dreampiler/kandev#227, head `e938540dc5`.

- Backend: `agent_error` dismiss kind with conservative identity match (404 on
  no match); `MarkAgentErrorFixed`; `ListAgentErrorInboxRows`; and
  `HasSuccessfulRunAfter`, applied to both `agent_run_failed` and `agent_error`;
  `GetInboxCount` now counts the computed failure items.
- Frontend: Mark-fixed control for `agent_error`, "mine" eligibility, extended
  dismiss kind, and both failure kinds counted in the sidebar badge.
- Verification: new backend unit tests and frontend Vitest tests pass;
  `go build ./...`, `go vet`, `golangci-lint --new-from-rev` (0 issues),
  `pnpm typecheck`, targeted `vitest`, `eslint`, `i18n:check`, and spec lints
  pass. Three pre-existing time-sensitive office tests fail on the base commit
  too (recorded as existing failures).
- Remaining inputs: none for this work order.

## Dependencies

None.
