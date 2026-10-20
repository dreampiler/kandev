---
created: 2026-10-20
status: implemented
requirements:
  - REQ-TASKS-WORKFLOW-DELETE-STEPS-001
system_design:
  - ../../specs/tasks/system-design/workflow-delete-steps.md
legacy_specs: []
---

# Implementation plan: Workflow delete removes steps

## Overview

Make workflow deletion remove the workflow's steps and clean up the steps that
earlier deletes left behind. One work order changes the two delete methods and
adds a one-time migration.

## Confirmed cause

- `DeleteWorkflow` deleted only `task_workflow_session_bindings` and
  `workflows`, relying on `ON DELETE CASCADE` for `workflow_steps`.
- The task repository initializes first and creates `workflow_steps` without
  a foreign key, so the workflow repository's cascading DDL never applies.
- Workspace deletion deletes steps explicitly and is not affected.

## Scope

### In scope

- Explicit step delete in `DeleteWorkflow` and `DeleteWorkflowsByWorkspace`.
- One-time orphan prune with a `kandev_meta` marker.

### Out of scope

- Repository initialization order and adding the foreign key.
- `workflow_step_participants` rows of deleted steps.

## Work orders

- [completed] [Task 01: Delete workflow steps](task-01-delete-workflow-steps.md)

## Verification strategy

Repository tests on an on-disk SQLite database through `NewWithDB`, so the
table under test is the same stub production uses. They must fail on the base
commit and pass after. An environment-gated PostgreSQL test covers the same
delete and migration.

## Verification results

Recorded in the task work order.

## Risks

- A test that seeds steps without a workflow and then reopens the repository
  on a database without the marker would see those steps removed. Fresh
  databases write the marker on first boot, so seeding after boot is unaffected.
