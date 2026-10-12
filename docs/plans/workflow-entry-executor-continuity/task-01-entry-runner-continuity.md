# Task 01: Entry runner continuity

Status: in progress (source edits exist from the prior turn; verification and
entry proof pending — no new production/test edits before the four-artifact
checkpoint is accepted by the parent controller)

## Goal

A workflow-step entry resolves its executor from the task's ready canonical
environment (or explicit transition), never from a stale task-metadata
`executor_profile_id`, and attaches to the same task's existing environment so
one entry yields exactly one session and no second physical worktree.

## Acceptance criteria

- AC1: With a ready canonical local environment and stale Worktree metadata
  profile, session preparation and launch use the environment's executor; the
  launch metadata carries no stale profile.
- AC2: An explicit caller-selected runner transition is preserved end to end.
- AC3: With no environment, prior metadata/session-profile behavior is kept.
- AC4: `buildLaunchAgentRequest` executor type follows the bound environment.
- AC5: Exactly one session per entry; no `git worktree add` "already exists".

## Changes (file:line scope)

- `apps/backend/internal/orchestrator/executor/executor_execute.go`
  - `prepareSessionAttempt` (~:1568): insert `existingEnvironmentRunner` +
    `canonicalEnvironmentRunner` before `taskRunnerResolution`; clear stale
    `executor_profile_id` from launch metadata when no effective profile
    (~:1661 block).
  - New helper `existingEnvironmentRunner` (~:1929) and
    `canonicalEnvironmentRunner` (~:1948).
  - `preparedWorkspaceInventoryRequest` / `buildLaunchAgentRequest` (~:2779,
    ~:2875): replace metadata-profile copy with `bindSessionLaunchRunner` +
    `setLaunchMetadataProfile`.
- `apps/backend/internal/orchestrator/executor/task_runner_context.go`
  - `runnerRequestedDiffersFromTaskMetadata`: deliberate-selection boundary used
    by `canonicalEnvironmentRunner` and `bindSessionLaunchRunner`.
- `apps/backend/internal/orchestrator/executor/executor_entry_runner_continuity_test.go`
  (new, companion): `TestCanonicalEnvironmentRunnerPrefersEnvironmentUnlessSelected`,
  `TestBindSessionLaunchRunnerClearsStaleMetadataProfile`,
  `TestBuildLaunchAgentRequestFollowsCanonicalEnvironmentRunner`,
  `TestRunnerRequestedDiffersFromTaskMetadata`,
  `TestPrepareSessionEntryPrefersCanonicalEnvironmentRunner`.

## Guard correction

The initial guard keyed on `WithTaskRunnerProfileExplicit`, but the workflow
entry (`event_handlers_workflow.go:2195-2196`) and `startTask`
(`task_operations.go:1600`) flag a metadata-derived profile as explicit, so the
override never fired. The guard now compares the requested profile against the
task's stored runner metadata: an echoed/empty request loses to the canonical
environment, a deliberately different non-empty profile is preserved.

## Verification

- `go build -trimpath ./internal/orchestrator/...` (from `apps/backend`): OK.
- `go test -trimpath ./internal/orchestrator/executor/ -run '<changed names>'`:
  all five continuity tests pass, including the `PrepareSession` entry-path test.
- Relevant existing cases in the changed package run green; the sole failure
  (`TestSwitchModelFallbackReusesAuthoritativeKubernetesRuntime`) reproduces at
  the base revision with these edits stashed (POSIX kubeconfig path treated as
  non-absolute on Windows) and is unrelated.
- `gofmt -l` clean on the three changed files;
  `golangci-lint run ./internal/orchestrator/executor/ --new-from-rev=HEAD` = 0 issues.
- The standalone `scripts\dev-isolated.ps1` entry proof was not run in this
  environment; the in-package `PrepareSession` entry test is the available
  entry-path evidence. Recorded as a limitation.
- No blanket orchestrator/worktree suites, no full-repo lint, no E2E/CI.

## Work order sequence

1. Produce the four-artifact package (requirements + system-design reused;
   this plan + this work order created) and report the checkpoint to parent.
   Done.
2. Verify the source edits compile and pass the narrow tests. Done.
3. Complete the narrow cases within the changed package only. Done.
4. Isolated same-branch entry proof: not runnable here; in-package
   `PrepareSession` entry test substituted. Limitation recorded.
5. Conventional Commits, narrow review, push, fork PR (repo procedure);
   report source + entry outcome to parent and W091 for the upstream decision.

