---
created: 2026-10-20
status: implemented
requirements:
  - REQ-TASKS-WORKFLOW-VISIBILITY-PATCH-001
system_design:
  - ../../specs/tasks/system-design/workflow-visibility-patch.md
legacy_specs: []
---

# Implementation plan: Workflow visibility patch

## Overview

Let operators archive (hide) and restore workflows through the ordinary
workflow update instead of deleting them. One work order adds the optional
`hidden` field to REST and WebSocket requests, writes it through the existing
field-scoped workflow update, and rejects visibility changes on built-in
workflows.

## Confirmed cause

- `httpUpdateWorkflowRequest` and `wsUpdateWorkflowRequest` had no `hidden`
  field, so `{"hidden":true}` was silently dropped and returned `200`.
- `Service.SetWorkflowHidden` exists but is only called by system flows.
- `DELETE /api/v1/workflows/:id` archives tasks and removes the row, so it
  cannot serve as an archive.
- Built-in rows (`is_system = 1`) get their visibility rewritten from the
  template at startup (`hideBuiltinWorkflows`, `ensureBuiltinWorkflow`).

## Scope

### In scope

- Optional `hidden` on REST `PATCH /api/v1/workflows/:id` and WebSocket
  `workflow.update`.
- Built-in rejection with a validation error.
- `is_system` read into the internal workflow model.
- The web PATCH payload type and public WebSocket API docs.

### Out of scope

- Archive controls in the settings UI.
- Bulk endpoints or changes to delete, template, or reconciliation behavior.

## Work orders

- [completed] [Task 01: Patch workflow visibility](task-01-patch-workflow-hidden.md)

## Verification strategy

Registered HTTP and WebSocket tests through the real router, dispatcher, SQLite
repository, and event bus. They must fail on the base commit before production
edits and pass after. Existing workflow handler, service, and repository tests
must stay green.

## Verification results

- RED on `origin/main` `9d57ce37a`: all six subtests of
  `TestRegisteredWorkflowHiddenPatchHTTP` and `TestRegisteredWorkflowHiddenPatchWS`
  failed (hidden not persisted; built-in change not rejected).
- GREEN after the change: all six subtests pass. On the Windows development
  host, the task handlers, service, SQLite repository, models, and Improve
  Kandev packages show the same failure set as the base commit: host-specific
  failures (file modes, symlinks, Git paths) and intermittent same-tick
  `UpdatedAt.After` assertions. No failure is introduced by this change.
- Specification catalog validation and full specification lint pass.

## Risks

- A built-in workflow hidden by its template could not be shown through this
  path. That is intended: startup reconciliation would revert it.
