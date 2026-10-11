---
id: "01-separate-automation-counts"
title: "Separate automation executions from the overview counts"
status: done
wave: 1
depends_on: []
plan: "plan.md"
spec: "../../specs/office/requirements/workspace-overview.md"
---

# Task 01: Separate Automation Executions from the Overview Counts

## Acceptance

- The Office overview created-in-window, completed-in-window, open-task, workspace-breakdown, and project-statistics reads exclude both `automation_run` and `automation_task`; an ordinary task (including a zero-cost one) stays counted, and the window boundary is respected.
- The Office projects page task count excludes both automation-execution origins; no other filter on that path changes.
- `ListOverviewAutomationTasks` and `ListOverviewAutomationCounts` select the same two-origin set.
- The project-statistics line renders the separate automation figure, localized, sharing the line's window and workspace scope.
- Task rows, states, and automation definitions are unchanged.

## Verification

```bash
cd apps/backend && go build ./...
cd apps/backend && go test ./internal/office/repository/sqlite/ ./internal/office/dashboard/
cd apps/web && pnpm run typecheck && pnpm run i18n:check
cd apps/web && pnpm exec vitest run app/office/overview/overview-activity-line.test.tsx
```

## Files likely touched

- `apps/backend/internal/office/repository/sqlite/tasks.go`
- `apps/backend/internal/office/repository/sqlite/aggregate.go`
- `apps/backend/internal/office/repository/sqlite/aggregate_overview.go`
- `apps/backend/internal/office/repository/sqlite/aggregate_overview_activity.go`
- `apps/backend/internal/office/repository/sqlite/projects.go`
- `apps/backend/internal/office/dashboard/overview.go`
- `apps/backend/internal/office/dashboard/overview_activity.go`
- `apps/backend/internal/office/dashboard/overview_types.go`
- `apps/web/app/office/overview/overview-activity-line.tsx`
- `apps/web/lib/state/slices/office/overview-types.ts`
- `docs/specs/office/requirements/workspace-overview.md`
- `docs/specs/office/system-design/workspace-overview.md`
- `docs/specs/office/requirements/automation-target-modes.md`

## Dependencies

None.

## Risks

- Do not widen the shared `andNotAutomationOrigin` predicate: it is used outside the counts (runner projections, blockers) and must keep its run-only meaning.
- Do not add filters (for example an ephemerality filter) to a path that did not have one.
- Do not touch analytics, Kanban, or the sidebar.

## Output contract

Report the classification, the window and project scope, the changed files, and exact test results; update this task and `plan.md`.

## Results

Implemented in fork PR https://github.com/dreampiler/kandev/pull/234 (`dreampiler/kandev`, base `main`, head `feature/24-311`). One predicate applied to the count reads; the separate automation figure is reported on the project-statistics line and localized with the existing `common:automation` copy.
