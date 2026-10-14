---
id: "01-register-and-route"
title: "Register a recovery recipient and route the next initial entry"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WORKFLOW-PROFILE-SESSIONS-004
acceptance_criteria:
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.1
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.2
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.3
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.4
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.5
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-004.6
system_design:
  - ../../specs/tasks/system-design/recovery-recipient-binding.md
---

# Task 01: Register and route a recovery recipient

## Summary

Implement the explicit recovery option on existing primary promotion, atomically
store its binding, and consume it only for a new initial-target entry. Prove the
supported action reaches the actual next-step prompt with isolated task data.

## In scope

- Typed binding and repository validation in the existing task persistence owner.
- Optional `replaces_initial_recipient_session_id` on `session.set_primary`,
  reusing authorization, primary transaction, admission and queue-transfer helpers.
- Shared effective recipient selection across preview/preflight and all workflow
  entry paths, preserving exact same-entry route retry and original configuration.
- Relevant existing companion tests and the existing public sessions/workflow guide.
- Shared frontend preview-revision metadata/profile projection and its existing
  unit test; preserve the historical original-session reference.

## Out of scope

Production data, installation, live incident recreation, generic metadata editing,
new MCP tools/UI controls, schema migration, new test suites/gates/pollers, model
or ceiling changes, Office recovery, upstream posting, and unrelated fork work.

## Acceptance

1. The supported explicit operation validates expected source/provenance and
   eligible replacement, commits primary/binding atomically, handles exact retry
   and competing/stale successor attempts, and leaves ordinary promotion unchanged.
2. A new initial entry resolves the registered logical recipient/profile through
   preview, preflight and entry; an old entry replays its route. Original history,
   direct-profile/source-step targets, dynamic order, queue contracts and barriers
   remain intact. Restart/workflow change retain the binding; task copies omit it.
3. In one isolated handler-to-engine flow, the actual next-step prompt reaches
   the replacement exactly once. Invalid/missing provenance rejects without
   inventing a snapshot. Record integration evidence separately from deployment.

## Verification

From `apps/backend`, run each command separately:

```text
go test -trimpath ./internal/orchestrator ./internal/orchestrator/handlers ./internal/task/repository/sqlite ./internal/task/models ./internal/task/service -run 'RecoveryRecipient|ProtectedTaskMetadata|WorkflowSessionTarget|ResolveInitialWorkflowSession|LoadRecordedWorkflowSessionRoute|PrepareExplicitWorkflowSession|SetPrimarySession|PreviewWorkflowSessionTarget|PreviewWorkflowMove_DoesNotBackfillLegacyInitialSessionMetadata' -count=1
go test -trimpath -race ./internal/task/repository/sqlite ./internal/orchestrator -run 'RecoveryRecipient' -count=1
go run ./cmd/sqlguard ./internal
go test -trimpath -race ./internal/persistence/storeconformance -count=1
```

Use the established build tags if required by the existing local SQLite setup;
record the exact executed command rather than assuming success. If PostgreSQL is
available through `KANDEV_TEST_POSTGRES_DSN`, run the same new transaction cases
there; otherwise report PostgreSQL unverified without introducing a new service.

From `apps/web`, reuse the existing hook unit test:

```text
pnpm exec vitest run hooks/domains/kanban/use-workflow-move-preview-revision.test.ts
pnpm exec eslint hooks/domains/kanban/use-workflow-move-preview-revision.ts
```

If this worktree has no dependencies, first run `pnpm install --frozen-lockfile`
from `apps`. This is shared state-only logic with unchanged desktop/phone rendering;
the mobile-parity state-only exception applies. No new permanent E2E is planned.

From the repository root, run separately:

```text
python -X utf8 scripts/list-docs.py validate
python -X utf8 scripts/lint-spec-files.py --all
git diff --check
```

The existing PR documentation coverage preflight must accept the linked package.
Use its exported `validateCoverage` with actual changed paths and referenced file
contents; do not publish a status or add a new checker. Run the existing public-doc
check only for the guide changed by this work order.

Evidence mapping: existing `workflow_session_target_test.go` covers entry replay,
exact targeting, terminal/deleted fallback and source binding; extend its missing
recovery cases for criteria 004.2/.3/.5. Existing repository primary/binding tests
cover atomicity, expected-source competition and retry for 004.1/.3/.4/.5.
Existing preview tests cover read-only agreement and missing provenance. Extend
the existing orchestrator handler tests with a `RecoveryRecipient` integration
case for 004.6, using real task repository and workflow entry and a fake external
agent. Inspect prompt recipient/content and immutable records, not only counters.
Cover different logical profiles, same-entry committed retry, ordinary spawn or
promotion without registration, and a stale A-to-B attempt after B-to-C.
Include the existing conditional `start_created` profile-change path in the
no-implicit-succession assertions: workflow profile precedence, immutable snapshot
profile, and the recorded-route profile guard must remain effective. Do not turn
that path into a generic profile PATCH or relax production checks to repair a
missing test profile fixture. Reuse existing companion assertions; no new suite.

## Files likely touched

- `apps/backend/internal/task/models/` (typed binding and duplication filtering)
- `apps/backend/internal/task/service/service_task_metadata.go` (existing create protection)
- `apps/backend/internal/task/repository/sqlite/session.go` and a small binding helper
- `apps/backend/internal/orchestrator/task_operations.go`
- `apps/backend/internal/orchestrator/task_session_primary_queue.go`
- `apps/backend/internal/orchestrator/handlers/handlers.go`
- `apps/backend/internal/orchestrator/workflow_session_target.go`
- `apps/backend/internal/orchestrator/workflow_move_preview.go`
- `apps/web/hooks/domains/kanban/use-workflow-move-preview-revision.ts` and its test
- Existing companion primary/binding, workflow-target, preview and handler test files
- `docs/public/sessions-and-review.md` or the owning existing workflow guide
- This package and its linked requirements/design

Confirm exact test-host file and task-copy metadata helper before editing; do not
create a second duplication path. Keep new production helpers small and reuse
existing metadata and transaction functions.

## Dependencies

None beyond the source/design investigation recorded in [the plan](plan.md).
Use the existing task/session and review/integration path. No delegation is
authorized. A later equivalent upstream implementation supersedes duplicate work.

## Risks

Source liveness and expected binding must be rechecked at the durable write;
same-entry replay must precede mutable profile resolution. Do not treat unrelated
live reviewers, a changed primary, or missing initial metadata as recovery proof.

## Parallelism

`sequential`

## Inputs

- [Requirement 004](../../specs/tasks/requirements/workflow-profile-session-lifecycle.md)
- [Recovery binding design](../../specs/tasks/system-design/recovery-recipient-binding.md)
- Existing incident report and upstream evidence in [the plan](plan.md)
- Scoped backend guidance and existing primary/route transaction test patterns

## Results

Implemented following the later direct implementation request. The supported WS
registration and real repository/engine/step-launch fixture delivers the actual
prompt exactly once to the replacement execution. It also proves ordinary primary
promotion creates no binding and registration preserves initial history and the
prior committed route. Source-step profile changes retain their existing behavior;
dynamic registration preserves logical identity without choosing a candidate.

Focused ordinary and race Go checks passed, including the final affected
orchestrator/handler tests after the last production edit. Task-owned SQLite store
conformance, the new SQL file's strict guard scan, changed-package Go lint, 11
preview-hook tests, changed-file ESLint, documentation catalog/spec lint, diff
check and PR coverage references passed. Public-doc validator tests passed (63).

Full SQL guard, full public-doc coverage and full store conformance have the
baseline failures detailed in [the plan](plan.md#verification-results). Storage
path and v0.93.0 upgrade failures, plus the original missing-ep1 fixture failure,
were reproduced on unchanged HEAD using temporary Go overlays. The relevant ep1
fixture was corrected; no unrelated baseline source was changed. PostgreSQL is
unverified because its DSN is unset. No new permanent suite, collector or gate was
introduced. No operating deployment is claimed.

Publication and independent review follow this work order through the native Task
Plan's actual PR/head/base/worktree records. Fork-main merge requires controller GO.
