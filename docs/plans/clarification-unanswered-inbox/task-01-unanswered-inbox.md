---
id: "01-unanswered-inbox"
title: "Unanswered clarification inbox"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-CLARIFICATION-UNANSWERED-001
acceptance_criteria:
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.1
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.2
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.3
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.4
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.5
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.6
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.7
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.8
  - AC-TASKS-CLARIFICATION-UNANSWERED-001.9
system_design:
  - ../../specs/tasks/system-design/clarification-active-lifecycle.md
---

# Task 01: Unanswered clarification inbox

## Summary

Keep questions an agent stopped waiting on answerable as ordinary messages while the workspace opts
in, close exactly the question a late answer answered, and let agents see them before asking again.

## In scope

- `workspaces.clarification_unanswered_inbox_enabled` and
  `workspaces.clarification_unanswered_retention_days` through schema, migration, repository,
  service validation, DTO, HTTP/WS update, events, boot state, and the web store.
- `ListUnansweredClarificationBundles`, `CloseUnansweredClarificationBundle`, and
  `MergeUnansweredClarificationDuplicates` in a new repository file, with lazy retention.
- `GET /api/v1/clarification-inbox/unanswered`, `POST .../unanswered/:pendingId/close`, and
  `POST .../unanswered/:pendingId/late-answer`.
- `include_unanswered` and per-bundle `state` on `list_pending_questions_kandev`.
- The Needs-you unanswered section, the late-answer hook's target and close, and the settings card.

## Acceptance

1. With the setting off, no unanswered rows, merges, retention writes, extra requests, or `state`
   fields appear; existing lifecycle tests stay green.
2. With it on, a superseded pending question is listed until it is answered late, closed, merged,
   expired by retention, or its task is archived; terminal-session expiry and parent questions stay
   excluded, and reads stay workspace-scoped.
3. A late answer closes only its own question and defaults to the asking session; another session of
   the same task is accepted only after the asking session ended.

## Verification

```bash
(cd apps/backend && go test ./internal/task/repository/sqlite -run 'Unanswered' -count=1)
(cd apps/backend && go test ./internal/task/repository/sqlite -run 'Workspace|Clarification|Inbox|PendingAction|PendingInteraction|History' -skip 'DeliveryRetention|MemoryLinux' -count=1 -timeout 40m)
(cd apps/backend && go test ./internal/clarification -count=1)
(cd apps/backend && go test ./internal/mcp/handlers -run 'PendingQuestions|AnswerQuestion' -count=1)
(cd apps/web && pnpm exec vitest run components/needs-you-inbox hooks/use-late-clarification-message app/settings/workspace/workspace-unanswered-inbox lib/ws/handlers/workspaces)
(cd apps/web && pnpm run i18n:check)
```

Playwright: `apps/web/e2e/tests/chat/needs-you-inbox-unanswered.spec.ts` (run in CI).

## Completion report

- Repository: 13 SQLite cases cover the setting gate, active exclusion, late-answer isolation,
  merge by text and by agent id, positional-id rejection, terminal expiry, archive and unarchive,
  retention, parent questions, workspace scope, sidecar pagination, and settings round trip. A
  PostgreSQL counterpart skips without `KANDEV_TEST_POSTGRES_DSN`.
- Handlers and MCP: the inbox endpoint, close, late-answer targets, authorization, and the MCP
  option's default, merge order, cursor, automation self-view, and caller scope are covered.
- Remaining risk: the Playwright spec and the PostgreSQL case were not run locally.

## Files likely touched

- `apps/backend/internal/task/repository/sqlite/clarification_unanswered_query.go`
- `apps/backend/internal/clarification/inbox_unanswered_handlers.go`
- `apps/backend/internal/mcp/handlers/question_unanswered.go`
- `apps/web/components/needs-you-inbox/needs-you-inbox-unanswered-section.tsx`
- `apps/web/app/settings/workspace/workspace-unanswered-inbox-settings.tsx`
