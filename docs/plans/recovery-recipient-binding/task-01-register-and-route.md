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
- Existing tests and necessary existing assertion/fixture updates; additional
  recovery checks are temporary. Update the existing public sessions/workflow guide.
- Shared frontend preview-revision metadata/profile projection and its existing
  unit test; preserve the historical original-session reference.

## Out of scope

Production data, installation, live incident recreation, generic metadata editing,
new MCP tools/UI controls, schema migration, new permanent test functions/cases,
test suites/gates/pollers, model
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
go test -trimpath -p 1 ./internal/orchestrator ./internal/orchestrator/handlers ./internal/task/repository/sqlite ./internal/task/models ./internal/task/service -run 'ProtectedTaskMetadata|WorkflowSessionTarget|WorkflowSessionBinding|ResolveInitialWorkflowSession|LoadRecordedWorkflowSessionRoute|PrepareExplicitWorkflowSession|SetPrimarySession|PreviewWorkflowSessionTarget|PreviewWorkflowMove_DoesNotBackfillLegacyInitialSessionMetadata|RestoreRequiredRecoveryResponse|WSLaunchSession_DurableRecovery|LoadOriginalSessionEffectiveConfiguration|IsOriginalTaskSession' -count=1
```

Use the established build tags if required by the existing local SQLite setup;
record the exact executed command rather than assuming success. If PostgreSQL is
available through `KANDEV_TEST_POSTGRES_DSN`, the temporary transaction checks may
use it; otherwise report PostgreSQL unverified without introducing a new service.
Do not require unrelated full SQL guard, store-conformance or race audits.

Missing recovery checks run once from a standalone tracked-source copy under
`D:/260720_Ai_Workspace/_Runtime/tmp`, not from this worktree's collectors. Keep
the already-created recovery checks only in that temporary copy, run the narrow
`RecoveryRecipient` filter for orchestrator, handlers, repository and service,
record the actual command/results, then remove the copy. The existing RED/GREEN
evidence remains valid when temporary files are cleaned up. No new function/case
is retained in normal collectors or in the final PR diff.

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
exact targeting, terminal/deleted fallback and source binding. Temporary recovery
checks supply missing coverage for criteria 004.2/.3/.5 and atomicity,
expected-source competition and retry for 004.1/.3/.4/.5.
Existing preview tests cover read-only agreement and missing provenance. Run the
temporary `RecoveryRecipient` handler integration check for 004.6, using real
task repository and workflow entry and a fake external
agent. Inspect prompt recipient/content and immutable records, not only counters.
Cover different logical profiles, same-entry committed retry, ordinary spawn or
promotion without registration, and a stale A-to-B attempt after B-to-C.
Include the existing conditional `start_created` profile-change path in the
no-implicit-succession assertions: workflow profile precedence, immutable snapshot
profile, and the recorded-route profile guard must remain effective. Do not turn
that path into a generic profile PATCH or relax production checks to repair a
missing test profile fixture. Reuse existing assertions; do not retain new cases.

## Files likely touched

- `apps/backend/internal/task/models/` (typed binding and duplication filtering)
- `apps/backend/internal/task/service/service_task_metadata.go` (existing create protection)
- `apps/backend/internal/task/repository/sqlite/session.go` and a small binding helper
- `apps/backend/internal/orchestrator/task_operations.go`
- `apps/backend/internal/orchestrator/task_session_primary_queue.go`
- `apps/backend/internal/orchestrator/handlers/handlers.go`
- `apps/backend/internal/orchestrator/workflow_session_target.go`
- `apps/backend/internal/orchestrator/workflow_move_preview.go`
- `apps/web/hooks/domains/kanban/use-workflow-move-preview-revision.ts`
- Existing test files only for necessary existing assertion/fixture updates
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

Implemented under the owner's original conditional execution instruction and the
existing Task 01 scope. This does not claim a new post-plan owner answer. The supported WS
registration and real repository/engine/step-launch fixture delivers the actual
prompt exactly once to the replacement execution. It also proves ordinary primary
promotion creates no binding and registration preserves initial history and the
prior committed route. Source-step profile changes retain their existing behavior;
dynamic registration preserves logical identity without choosing a candidate.

Initial implementation verification at `fd68fe777918`: focused ordinary and race
Go checks passed, including the final affected
orchestrator/handler tests after the last production edit. Task-owned SQLite store
conformance, the new SQL file's strict guard scan, changed-package Go lint, 11
preview-hook tests, changed-file ESLint, documentation catalog/spec lint, diff
check and PR coverage references passed. Public-doc validator tests passed (63).

Full SQL guard, full public-doc coverage and full store conformance have the
baseline failures detailed in [the plan](plan.md#verification-results). Storage
path and v0.93.0 upgrade failures, plus the original missing-ep1 fixture failure,
were reproduced on unchanged HEAD using temporary Go overlays. The relevant ep1
fixture was corrected; no unrelated baseline source was changed. PostgreSQL is
unverified because its DSN is unset. The initial implementation incorrectly left
new cases in existing collector files. The owner clarified that these are also
prohibited permanent additions; they are removed from the final diff, with the
existing ep1 fixture correction retained. Recovery evidence is preserved through
temporary standalone-source checks and the plan's recorded RED/GREEN results.
The temporary recovery checks passed in all four packages and the owned source
copy/archive were recycled. Existing targeting/primary/metadata, handler recovery,
repository binding and original-session model checks passed in the worktree.
The restored hook file passed 10 existing tests. No production code changed during
this correction. Exact commands and the initial no-match portions are recorded in
[the plan](plan.md#test-lifetime-correction-2026-10-15-kst).
No operating deployment is claimed.

The owner's later source review found a required legacy edge case: after A-to-B
registration without an initial snapshot, deleting A allowed original-session
lookup and B-to-C registration to re-elect B. The repair reads the recorded
historical ID from the existing snapshot or protected typed binding before legacy
election, preserving a missing original/configuration and the absent snapshot.
Preview, entry, re-registration, repository validation and shared preview revision
use the same history rule. Temporary deletion/fallback/chain checks and a real
legacy WS-to-next-prompt check cover it outside normal collectors; exact RED/GREEN
and publication/review evidence is retained in the plan and native Task Plan.

Publication and independent review follow this work order through the native Task
Plan's actual PR/head/base/worktree records. Fork-main merge requires controller GO.
