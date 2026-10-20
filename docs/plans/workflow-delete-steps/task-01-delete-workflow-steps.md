---
id: "01-delete-workflow-steps"
title: "Delete workflow steps"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WORKFLOW-DELETE-STEPS-001
acceptance_criteria:
  - AC-TASKS-WORKFLOW-DELETE-STEPS-001.1
  - AC-TASKS-WORKFLOW-DELETE-STEPS-001.2
  - AC-TASKS-WORKFLOW-DELETE-STEPS-001.3
  - AC-TASKS-WORKFLOW-DELETE-STEPS-001.4
system_design:
  - ../../specs/tasks/system-design/workflow-delete-steps.md
---

# Task 01: Delete workflow steps

## Summary

Delete a workflow's steps inside the workflow delete transaction and prune
existing orphan steps once on upgrade.

## In scope

- `DELETE FROM workflow_steps WHERE workflow_id = ?` in `DeleteWorkflow`.
- The same delete for the selected workflows in `DeleteWorkflowsByWorkspace`.
- `pruneOrphanWorkflowStepsOnce` called from `runMigrations`.

## Out of scope

- Initialization order, foreign keys, and participant rows.

## Acceptance

- Deleting a workflow removes its steps and keeps other workflows' steps.
- A not-found delete removes no steps.
- The bulk delete removes only the deleted workflows' steps.
- The first boot without the marker removes orphan steps and writes the marker;
  a later boot keeps existing steps.

## Verification

```bash
# From apps/backend:
go test -trimpath -tags fts5 -p=1 ./internal/task/repository/sqlite -run 'TestDeleteWorkflow_|TestDeleteWorkflowsByWorkspace_RemovesTheirSteps|TestMigration_PrunesOrphanWorkflowStepsOnce|TestPostgresDeleteWorkflowRemovesStepsAndPrunesOrphans' -count=1
# From the repository root:
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
```

## Files likely touched

- `apps/backend/internal/task/repository/sqlite/workflow.go`
- `apps/backend/internal/task/repository/sqlite/workflow_step_orphan_migration.go`
- `apps/backend/internal/task/repository/sqlite/base_migrations.go`
- `apps/backend/internal/task/repository/sqlite/workflow_delete_steps_test.go`
- `apps/backend/internal/task/repository/sqlite/workflow_delete_steps_postgres_test.go`

## Dependencies

- None.

## Risks

- See the plan.

## Parallelism

`sequential`

## Inputs

- [Requirement](../../specs/tasks/requirements/workflow-delete-steps.md)
- [System design](../../specs/tasks/system-design/workflow-delete-steps.md)

## Results

- RED at base `e29a5b364` (clean detached worktree, new tests copied in
  unchanged): `TestDeleteWorkflow_RemovesItsSteps` (2 steps left, want 0),
  `TestDeleteWorkflowsByWorkspace_RemovesTheirSteps` (1 left, want 0) and
  `TestMigration_PrunesOrphanWorkflowStepsOnce` (2 orphan steps, want 0)
  failed. `TestDeleteWorkflow_NotFoundKeepsSteps` passed before and after, as
  the rollback control.
- GREEN from `apps/backend`:
  `go test -trimpath -tags fts5 -p=1 ./internal/task/repository/sqlite -run 'TestDeleteWorkflow_|TestDeleteWorkflowsByWorkspace_RemovesTheirSteps|TestMigration_PrunesOrphanWorkflowStepsOnce|TestPostgresDeleteWorkflowRemovesStepsAndPrunesOrphans' -count=1`
  passed (4 tests, `ok ... 1.815s`).
  `TestPostgresDeleteWorkflowRemovesStepsAndPrunesOrphans` was skipped:
  `KANDEV_TEST_POSTGRES_DSN` is unset and no local PostgreSQL is available.
- Fixture update verified with
  `go test ./internal/persistence/storeconformance -run TestUpgradeFixtureManifest`
  (PASS): the new sha256 values match `sqlite.sql` and `postgres.sql`, and the
  added `workflows` row keeps the step sentinel valid. That package's
  `TestStoreConformance` and `TestPreviousStableUpgrade` fail on
  `storage_temp_artifacts` ("temporary artifact path must be an absolute path")
  at the base commit too, so the failure is pre-existing and unrelated.
- `gofmt -l` on the touched files: no output.
  `bash scripts/lint-go-changed <files>`: 0 issues.
- `python3 scripts/list-docs.py validate` (392 decisions, 1670 specifications)
  and `python3 scripts/lint-spec-files.py --all` passed.
