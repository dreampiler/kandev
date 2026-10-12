# Workflow Entry Executor Continuity — Plan

Status: implemented; delivery in progress (design package produced; narrow verification green; fork PR pending)
Initiative: workflow-entry-executor-continuity
Worktree: `C:/Users/dream/.kandev/tasks/rbuff8ah/kandev` (branch `feature/08ddc5af-453e-4792-8-t5b`, fork main base)

## Owner goal and confirmed result

Owner goal (Operations, 2026-10-12): the workflow-step entry ("칸 진입") must
follow the task's actual executor/canonical environment, safely reuse the same
task's existing worktree, and produce exactly one session per entry, so the
`git worktree add` "already exists" failure does not repeat.

## Owning contract (reused, not amended)

The defect is already fully covered by the owning requirement/design pair, so no
spec amendment is made:

- `docs/specs/tasks/requirements/additional-session-workspace-reuse.md`
  - REQ-001: additional sessions attach only to a ready canonical environment,
    attach-only, never create a second workspace (AC-001.1–001.4).
  - REQ-003 AC-003.1: an executor-type mismatch is not an attach; it must select
    or materialize an environment owned by the requested executor.
  - Workspace-state matrix (lines 135–156): `new_workspace` + ready environment →
    attach with `reuse_required`; executor transition ≠ attach.
- `docs/specs/tasks/system-design/additional-session-workspace-reuse.md`
  - Canonical environment and inventory (lines 38–55): `task_environments` owns
    the workspace; a session row or cached projection is never a workspace
    authority.
  - Executor transition (lines 82–105): transition must select a ready
    environment owned by the requested executor or elect one fresh materializer;
    the previous environment's path/handles are diagnostic only.
  - Launch admission (lines 107–134): executor-type mismatch or invalid
    workspace fails closed with a typed recoverable error before agent start.

The missing repository artifact was the plan/work-order; this package is it.

## Root cause (pinned from source)

The workflow-entry auto-start path resolves the runner from task metadata
(`orchestrator/event_handlers_workflow.go:2195-2196` →
`resolveExecutorConfig` in `orchestrator/executor/executor_state.go:126-215`,
where `applyProfile` lets a profile override the executor/type). A stale
metadata `executor_profile_id` (e.g. Worktree) overrides the task's ready
canonical environment executor (e.g. local), so the new session targets the
wrong backend, finds no reusable worktree row for its new session id, and falls
through to `git worktree add` on an occupied path → `already exists`
(`worktree/manager_lifecycle.go:377-459`, `worktree/errors.go:70-80`).

## Approach (approved scope: A + bounded guard B)

- A. Environment-first resolution at the entry session boundary: when the task
  has a canonical environment, resolve executor/executor-profile from that
  environment first; metadata precedence remains only for the no-environment
  initial materializer. Explicit caller-selected runner transitions are
  preserved unchanged.
- B. Fail-closed at the same boundary: the launch metadata must not resurrect a
  stale profile that would authorize a second materialization under the wrong
  backend. No deletion/reset of foreign or name-colliding paths.
- Not chosen: central rewrite of `resolveExecutorConfig`/`prepareSessionAttempt`
  for all launch paths (Office/automation/review-watch risk).

### Guard correction (implementation note)

The first cut guarded the override with `WithTaskRunnerProfileExplicit`, but the
workflow-entry auto-start path reads the profile from task metadata and hands it
back to `startTask` (`event_handlers_workflow.go:2195-2196`), and `startTask`
sets that flag from `executorProfileID != ""` (`task_operations.go:1600`). The
metadata-derived profile therefore arrived flagged "explicit" and the guard
skipped the override: a no-op for the defect it was meant to fix.

The guard is now data-based, not flag-based: the environment wins unless the
caller deliberately selected a profile that differs from the task's stored
runner (`runnerRequestedDiffersFromTaskMetadata`). A request that merely echoes
`task.Metadata["executor_profile_id"]` is stale evidence once an environment
exists (the runner is frozen), while a genuinely different non-empty profile is
an executor transition and is preserved. This covers every entry path (workflow
step entry, queued auto-start, frontend echo) without a central behavior change
for deliberate transitions.

## Scope

In scope (`apps/backend` only, one fork PR):

- `internal/orchestrator/executor/executor_execute.go`
  - `prepareSessionAttempt`: before runner resolution, when the task has a
    canonical environment, override executor/profile with the environment's
    unless the caller deliberately selected a different profile; project the
    effective profile (empty = local default) onto launch metadata.
  - `preparedWorkspaceInventoryRequest` / `buildLaunchAgentRequest`: bind the
    launch runner from the bound environment + session so a stale metadata
    profile cannot reach `applyProfile`.
- `internal/orchestrator/executor/task_runner_context.go`
  - `runnerRequestedDiffersFromTaskMetadata`: the data-based deliberate-selection
    boundary shared by the two seams.
- Companion unit tests next to the changed logic:
  `internal/orchestrator/executor/executor_entry_runner_continuity_test.go`.

Out of scope: W091 live mutation (move/runner/restart/stop), operating install,
all-project workflow/prompt application (Operations/AIWS), upstream writes
(W091 only), new E2E/CI/validation frameworks, whole-project launch-path
rewrite.

## Validation (narrowest sufficient)

- Changed-logic unit tests (`go test -trimpath
  ./internal/orchestrator/executor/ -run '<changed names>'`):
  - `TestCanonicalEnvironmentRunnerPrefersEnvironmentUnlessSelected`
  - `TestBindSessionLaunchRunnerClearsStaleMetadataProfile`
  - `TestBuildLaunchAgentRequestFollowsCanonicalEnvironmentRunner`
  - `TestRunnerRequestedDiffersFromTaskMetadata`
  - `TestPrepareSessionEntryPrefersCanonicalEnvironmentRunner` — the real
    no-session entry path (`PrepareSession`) with a ready local canonical
    environment and stale worktree metadata profile yields a local session and
    clears the stale launch profile.
- Relevant existing cases in the changed package (transition/reuse/runner
  selection) run green; the only failure is
  `TestSwitchModelFallbackReusesAuthoritativeKubernetesRuntime`, reproduced at
  the base revision with the same edits stashed: it treats the POSIX
  `/etc/kandev/current-kubeconfig` as non-absolute on Windows. Pre-existing,
  environment-specific, unrelated to this change.
- `gofmt -l` clean on the three changed files; `golangci-lint run
  ./internal/orchestrator/executor/ --new-from-rev=HEAD` reports 0 issues.
- The standalone isolated entry proof (`scripts\dev-isolated.ps1`) was not run
  in this environment; the in-package `PrepareSession` entry test is the
  available entry-path evidence. Recorded as a limitation, not as a pass.

## Delivery (2026-10-12)

Re-verified on the delivery session before committing: the five continuity
tests pass, `go build -trimpath ./internal/orchestrator/executor/` exits 0, and
`gofmt -l` is clean on the three changed files. The change resolves the runner
at both seams (`prepareSessionAttempt`, `buildLaunchAgentRequest` /
`preparedWorkspaceInventoryRequest`), so a workflow-step entry follows the
task's ready canonical environment instead of a stale task-metadata
`executor_profile_id`; a deliberately selected different profile (a real
executor transition) is preserved.

- Commit: `f87bb80a9317f0176ad764b39a630840574995ed`
- Fork PR: https://github.com/dreampiler/kandev/pull/247 (based on `main`)

