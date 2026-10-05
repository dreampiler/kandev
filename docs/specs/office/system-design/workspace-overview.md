---
status: draft
system: office
requirements:
  - REQ-OFFICE-WORKSPACE-OVERVIEW-001
  - REQ-OFFICE-WORKSPACE-OVERVIEW-002
  - REQ-OFFICE-WORKSPACE-OVERVIEW-003
created: 2026-09-29
owners:
  - kandev
---

# Office Workspace Overview System Design

## Purpose and boundaries

This design defines the read-only overview for several workspaces.
The Office system owns the aggregate view. The task service owns workspace visibility. The task and Office repositories own their existing data.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-OFFICE-WORKSPACE-OVERVIEW-001` | Components, control flow, and security |
| `REQ-OFFICE-WORKSPACE-OVERVIEW-002` | Scope |
| `REQ-OFFICE-WORKSPACE-OVERVIEW-003` | Overview sections, statuses, and resource use |

## Components and responsibilities

- `apps/backend/internal/backendapp/office_scope.go` marks the aggregate route as a user-scoped route. It rejects agent callers and checks for a real user identity when authentication is enabled.
- `apps/backend/internal/office/dashboard/aggregate.go` gets workspaces from the task service. It filters for Office workspaces, sorts them by name, and builds the response.
- `apps/backend/internal/office/repository/sqlite/aggregate.go` reads task counts, approval counts, and recent activity in batches. It reads existing tables and adds no schema.
- `apps/backend/internal/office/dashboard/service.go` receives the identity-scoped workspace lister from the backend composition root.
- `apps/web/src/office-routes.tsx` maps `/office/overview` to the overview page. The Office shell provides the page title and navigation.
- `apps/web/hooks/domains/office/use-workspace-aggregate.ts` fetches the page data and refreshes it every 30 seconds while the page is mounted.
- `apps/web/lib/state/slices/office/office-slice.ts` owns the in-memory aggregate. The hook owns the local loading state and ignores stale responses.
- `apps/web/app/office/overview/workspace-aggregate-page-client.tsx` shows workspace cards and the merged activity feed. The shared activity row adds `workspaceId` to run links.

## Data and contracts

The frontend sends `GET /api/v1/office/workspaces/aggregate`. The response contains `workspaces` and `recent_activity`.

Each workspace entry contains its ID and name, task counts, pending approvals, agent count, and running-agent count. The activity list contains at most 20 entries across all returned workspaces.

The workspace rows come from the task service's `ListWorkspaces` call. The dashboard service keeps only rows with an Office workflow ID. The repository groups task and approval counts by workspace ID. It also selects the latest activity rows across the supplied IDs.

The overview state is not stored as a separate record. The database remains the source of task, approval, agent, and activity data. The frontend store keeps only the latest response in memory.

## Control flow

1. The user opens `/office/overview` from Office navigation.
2. The page hook calls the overview API with `cache: "no-store"`.
3. The backend route guard rejects agent tokens. When authentication is enabled, it also rejects missing or synthetic user identities.
4. The dashboard service calls the task service for the current caller's workspace list and keeps Office workspaces.
5. The service reads task counts, pending approvals, recent activity, and agent counts. It enriches activity labels and returns the aggregate.
6. The hook normalizes activity entries and writes the response to the Office store.
7. The page shows one card per workspace and one merged activity list. Workspace cards select a workspace through `workspaceId`.
8. The hook refreshes the response every 30 seconds while mounted.

## Scope

The user setting `office_overview_scope` is stored with the other user settings. `DashboardService` reads it through `OverviewScopeSource`. The user service implements that interface without the sidebar projection. The `office` scope keeps workspaces with an Office workflow ID. The `reachable` scope keeps the whole identity-scoped list. Both start from the task service's `ListWorkspaces`. When Organizations is enabled, that list is already bounded to the caller's organization.

## Overview sections, statuses, and resource use

- `apps/backend/internal/office/dashboard/overview.go` builds one snapshot per caller and scope. `overview_cache.go` keeps it for 20 seconds and joins concurrent misses with `singleflight`. The cache key carries the caller because the workspace list is identity-scoped.
- `apps/backend/internal/office/repository/sqlite/aggregate_overview.go` holds the reads. Each read takes a batch of workspace, task, or session IDs and runs on the read-only handle, one after another. Last output is one seek per live session on the `(task_session_id, author_type, created_at)` index. Step entry is one seek per open task on the `(task_id, occurred_at)` transition index. Message content and metadata are never selected.
- The completed read takes its instant from `task_step_transitions`: it narrows transitions by `occurred_at` through the occurred-at index, joins the completing steps (`complete_task_on_enter`), takes `MAX(occurred_at)` per task as the completion, and joins that to the tasks. One result feeds both the per-workspace figure and the list, so the two cannot disagree. `archived_at` travels with the row because most completed tasks have left the board.
- Answerable questions use the task repository's `ListAnswerableClarificationsForSessions`. It shares the inbox's answerable predicate and bounds the grouped scan to the live sessions of the snapshot.
- `overview_status.go` holds every time limit in `defaultOverviewThresholds`. It judges each open task from task and session states plus one step property: whether the step the task sits on starts work by itself (`models.StepRunsOnEntry`, which reads `on_enter` automation and `pull_from_step_id`). Step names, workflows, and people are never part of the rule. Each reason is a code with values.
- The hold count is the one place a step name is read, because nothing else identifies a hold step: `workflow_steps.stage_type` has no hold value, no boolean marks one, and a step that returns the task on a person's turn is shared with steps that are not holds. `holdStepNames` holds the names a workflow uses, compared case-insensitively; those names are workflow data an author typed rather than UI copy, so the reading does not change with the screen's language. A workflow that spells its hold step differently reports fewer holds, never more.
- A task's condition is read from its baseline session, picked as primary, then live, then newest, so a failed auxiliary session started moments ago cannot speak for a task whose primary session is still running. `latest` stays the newest session for the consumers that mean "most recent", such as the row's linked session and the failure look-back.
- A task is in progress only when its step runs by itself, its state is active, it has no unfinished blocker, nothing is running, no answer is owed. A task on a step that starts nothing, and a task waiting on an answer are waiting, not active work and not a delay. A parent with open children is read through those children: it is not delayed by its own step clock, and a child that is genuinely broken still reports its own status.
- A failure only makes a task an error while it is the newest thing that happened to it: no session completed after it, no agent output after it, and no later step entry. A failure the task has moved past is history.
- The dwell limit applies only to a step the task is meant to leave by itself. Hold, an open prerequisite, and a person are not step limits at all, so those tasks take none and `classifySteady` reads them as on hold or waiting; there is no separate hold limit to explain. The exclusions are by who owes the next move, never by elapsed time: only a step that starts nothing by itself, a task waiting for an answer, a task whose question is unanswered, and a parent with open children are exempt. A step that does start work by itself keeps its limit after its last turn ended, so a step that stopped advancing is still reported as a delay rather than as waiting.
- `loadAnswerableQuestions` runs before classification, once per pass, and both the dwell exemption and the needs-human list read that one result.
- `addTaskMetrics` and `taskMatchesFilter` both read those same predicates, so the metric tiles, the filtered lists, the warning row, and the reason text cannot disagree. No step-property threshold is re-derived in the browser.
- The open-task breakdown is one partition read once per task in `addTaskMetrics`: on hold, then waiting on an unfinished predecessor, then in progress, then the rest. `BlockedTasks` carries the hold count and a new `BlockedByTasks` carries the predecessor count, so the three card figures are three separate answers rather than one number read under two names, and `WaitingTasks` keeps meaning every open task that is not being driven. The card's third figure is `open_tasks - blocked_tasks - blocked_by_tasks`, which carries the tasks in progress along with the ones waiting on a person, so it is worded neutrally.
- `GET /api/v1/office/workspaces/aggregate/tasks` and `GET /api/v1/office/workspaces/aggregate/running` filter the same snapshot. The tasks route answers 404 for a workspace outside the snapshot. Only the queue list reads the head of each queue, once per snapshot.
- The page loads lists only while expanded. `use-workspace-aggregate.ts` and `use-overview-list.ts` refresh every 30 seconds, and only while the document is visible.

## Failure and recovery

If the workspace lister is not configured, the endpoint returns HTTP 503. If a repository read fails, the endpoint returns HTTP 500.

The page shows an error state when its first request fails. A periodic refresh retries the request. If a later request fails, the page keeps the last loaded response.

The hook uses a request generation to ignore responses after unmount. One in-flight request serves concurrent refresh calls.

## Persistence

The endpoint performs read-only queries. It adds no tables and writes no records. The browser keeps the response only in the in-memory Office store.

## Security

The aggregate route has no workspace ID in its path. The route guard therefore rejects agent callers before the authentication-disabled bypass. When authentication is enabled, the guard requires a real, non-synthetic user identity.

The dashboard service takes its workspace list from the task service. It does not build a separate user-to-workspace rule. The endpoint returns only Office workspaces from that list.

## Observability

The endpoint uses the existing Office HTTP logging and error reporting. It adds no metric or log identity labels.

Automation-created tasks carry `automation_id` in task metadata. The bounded automation-task query extracts only this identifier through the database dialect helper. Automation events open the originating workspace automation settings, while legacy rows without an identifier retain their task link.
