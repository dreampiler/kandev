---
id: "01-patch-workflow-hidden"
title: "Patch workflow visibility"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WORKFLOW-VISIBILITY-PATCH-001
acceptance_criteria:
  - AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.1
  - AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.2
  - AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.3
  - AC-TASKS-WORKFLOW-VISIBILITY-PATCH-001.4
system_design:
  - ../../specs/tasks/system-design/workflow-visibility-patch.md
---

# Task 01: Patch workflow visibility

## Summary

Accept an optional `hidden` field on the ordinary workflow update, persist it
through the field-scoped workflow write, and reject visibility changes on
built-in workflows.

## In scope

- `hidden *bool` on `httpUpdateWorkflowRequest`, `wsUpdateWorkflowRequest`,
  and `service.UpdateWorkflowRequest`.
- `ErrSystemWorkflowVisibility`, mapped to REST `400` and WebSocket
  `VALIDATION_ERROR`.
- `models.Workflow.IsSystem` read from `workflows.is_system`.
- Optional `hidden` in the web `updateWorkflowAction` payload type.
- Public WebSocket API partial-update section.

## Out of scope

- UI controls, bulk endpoints, delete/template/reconciliation changes.

## Acceptance

- Hide and unhide round trip over REST and WebSocket; tasks stay unarchived.
- Omitted `hidden` keeps the stored value while other fields apply.
- `workflow.updated` carries the persisted `hidden` value.
- Built-in visibility changes are rejected without writing other fields;
  the unchanged value is accepted.

## Verification

```bash
# From apps/backend:
go test -trimpath -tags fts5 -p=1 ./internal/task/handlers -run TestRegisteredWorkflowHiddenPatch -count=1
go test -trimpath -tags fts5 -p=1 ./internal/task/handlers ./internal/task/service ./internal/task/repository/sqlite ./internal/task/models ./internal/improvekandev -count=1
# From the repository root:
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
```

## Files likely touched

- `apps/backend/internal/task/handlers/workflow_handlers.go`
- `apps/backend/internal/task/handlers/workflow_hidden_patch_test.go`
- `apps/backend/internal/task/service/service_requests.go`
- `apps/backend/internal/task/service/service_resources.go`
- `apps/backend/internal/task/models/models.go`
- `apps/backend/internal/task/repository/sqlite/workflow.go`
- `apps/web/app/actions/workspaces.ts`
- `docs/public/websocket-api.md`

## Dependencies

- The field-scoped workflow write from the disjoint workflow edits plan.

## Risks

- Adding a column to the shared workflow select list affects every workflow
  read; the column already exists in both dialects and defaults to `0`.

## Parallelism

`sequential`

## Inputs

- [Requirement](../../specs/tasks/requirements/workflow-visibility-patch.md)
- [System design](../../specs/tasks/system-design/workflow-visibility-patch.md)

## Results

- RED on `9d57ce37a`: six registered subtests failed before production edits.
- GREEN: all six subtests pass; affected task and Improve Kandev packages pass.
