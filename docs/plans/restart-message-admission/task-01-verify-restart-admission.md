---
plan: plan.md
status: complete
depends_on: []
---

# Task 01: Verify restart message admission on base

## Goal

Determine whether fork main already satisfies the observable result (a message
to an execution-less eligible session after a backend restart starts a turn in
the same conversation, or surfaces explicit recovery with the message retained)
and reuse the existing fix if it does.

## Inputs

- Base worktree head: `6a6d3d76df1d2ca299f9774cd4f3223b8f94421b` (fork `main`,
  clean tree). Contains #3598 plus #4380, #4384, and #4403.
- Reported evidence is from the pre-#3598 rollback
  `c8bcf5e293-hotfix/start` and is treated as report-only, not as a contract.

## Method

Read the admission and recovery path, then run the base's focused restart and
message-admission tests. No network, no operating DB, no operating process, and
no server were started; the restart is simulated by constructing a fresh
orchestrator `Service` over an on-disk SQLite repository, which is the
repository's established restart-reproduction shape.

Key seams read:

- `apps/backend/internal/task/handlers/message_handlers.go`: `wsAddMessage`,
  `forwardMessageAsPrompt`, `handlePromptWithResume`,
  `queuePromptIfRuntimeUnavailable`, `markDeliveryMessageBlocked`.
- `apps/backend/internal/orchestrator/task_operations.go`: `promptTask`,
  `ensureSessionRunning*`, `attemptColdResume`.
- `apps/backend/internal/orchestrator/ceiling_seam3.go`:
  `classifyEnsureSessionRunningFailureForPrompt`.
- `apps/backend/internal/orchestrator/session_launch.go` and
  `session_native_delivery_recovery.go`: recovery surfacing and native-resume
  block acknowledgment.
- `apps/backend/internal/orchestrator/reconcile_liveness.go`:
  `pruneOrRepairExecutorRow`, resume-safety preservation.

## Commands and results (run from `apps/backend`)

| Command | Result |
| --- | --- |
| `go test ./internal/orchestrator/ -run 'TestRestartRecoveryRestoresNativeConversationWithoutPrompt\|TestContinuedRecoveryDoesNotRetryPreviousOwner\|TestSilentRestoreStopCancelsBlockedLaunchAndJoinsWorker' -count=1 -trimpath` | `ok github.com/kandev/kandev/internal/orchestrator 2.238s` |
| `go test ./internal/task/handlers/ -run 'TestForwardMessageAsPrompt' -count=1 -trimpath` | `ok github.com/kandev/kandev/internal/task/handlers 0.828s` |
| `go test ./internal/orchestrator/ -run 'TestPromptTask_ExecutorLookupErrorNotClassifiedRuntimeUnavailable\|TestPromptTask_ExhaustedColdResumeNotClassifiedRuntimeUnavailable\|TestPromptTask_NonRetryableRecoveryGuardRemainsVisible' -count=1 -trimpath` | `ok github.com/kandev/kandev/internal/orchestrator 2.112s` |
| `go test ./internal/orchestrator/handlers/ -run 'TestRestoreRequiredRecoveryResponseExposesUnresolvedDelivery\|TestWSLaunchSession_DurableRecovery' -count=1 -trimpath` | `ok github.com/kandev/kandev/internal/orchestrator/handlers 0.397s` |

All exit code 0. Toolchain: go1.26.0 (mise.toml).

## Evidence interpretation

- `TestRestartRecoveryRestoresNativeConversationWithoutPrompt` proves the
  restart path: startup restores generation 2 with the same native session id,
  writes no user message and no turn, preserves the queued entry while pausing
  auto-run, and a later ordinary `PromptTask` is delivered to the restored
  native conversation exactly once.
- `TestForwardMessageAsPrompt_*` and `TestPromptTask_*` pin admission: a
  genuine resume failure stays visible; the "no executor record" shape is the
  only class queued for a pending runtime; a non-retryable recovery guard stays
  visible.
- `TestRestoreRequiredRecoveryResponseExposesUnresolvedDelivery` and
  `TestWSLaunchSession_DurableRecovery` pin that an open durable block surfaces
  the explicit `continue_from_history` action and that showing recovery does
  not authorize it.

## Result

Verified. The base satisfies the observable result; the existing fix is reused
and no production or permanent-test change is made. See `plan.md` for the
finding and the operational remaining input.
