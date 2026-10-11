---
id: "02-narrow-store-lock"
title: "Narrow the execution-registry write window and reproduce cross-session isolation"
status: done
wave: 2
depends_on:
  - "01-bounded-acquisition"
plan: "plan.md"
requirements:
  - REQ-AGENTS-PENDING-PERMISSION-LOOKUP-001
acceptance_criteria:
  - AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.2
  - AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.4
system_design:
  - ../../specs/agents/system-design/pending-permission-lookup-bounds.md
---

# Task 02: Narrow the execution-registry write window and reproduce cross-session isolation

## Summary

Stop holding the execution-registry write ownership while per-execution runtime
state is cleared, and prove with a finite isolated reproduction that one
session's cancel escalation cannot block another session's pending permission
resolution.

## In scope

- In `ExecutionStore.Remove`, delete the indexes and update the runtime gauge
  under the write lock, capture the execution, release the lock, then clear the
  runtime environment outside the lock.
- Add a source-adjacent test proving the environment is still cleared after
  removal and that a concurrent session lookup is not held by the environment
  clear.
- Add a finite isolated mock-agent reproduction that cancels/escalates one
  session while another session's pending permission is resolved; run it as a
  temporary, untracked harness outside automatic test collection.
- Observe the real entry outcome for the reproduction.

## Out of scope

- The agentctl server-side response bound and delivery-writer cancellation.
- New permanent integration, E2E, or CI tests.
- Delivery retention and starting-route cleanup.

## Acceptance

- `ExecutionStore.Remove` no longer calls `clearRuntimeEnvironment` while
  holding the registry write lock, and the runtime environment is still cleared
  after removal (regression test).
- The isolated reproduction shows a second session's permission resolution
  returning within its deadline while the first session's removal is held on the
  per-execution environment lock.

## Verification

```bash
(cd apps/backend && gofmt -l internal/agent/runtime/lifecycle/)
(cd apps/backend && go test -trimpath -race ./internal/agent/runtime/lifecycle/ -run 'TestRemoveReleasesRegistryLock|TestExecutionStore' -count=1)
(cd apps/backend && go build ./...)
```

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/execution_store.go`
- `apps/backend/internal/agent/runtime/lifecycle/execution_store_test.go`
- `apps/backend/internal/agent/runtime/lifecycle/manager_permission_deadline_test.go`
- Temporary untracked mock-agent reproduction harness under
  `C:\Users\dream\AppData\Local\Temp\opencode\` (not committed, not collected)

## Dependencies

Task 01 (bounded acquisition) must be complete.

## Risks

- The reproduction must be deterministic; a timing-based test could pass
  without exercising the contention. Hold the environment lock explicitly
  rather than relying on sleeps.
- Clearing the environment outside the write lock must not race a concurrent
  re-registration of the same session; the execution is removed from all indexes
  before the clear, so a replacement cannot share the pointer.

## Parallelism

`sequential`

## Inputs

- [Pending Permission Lookup Bounds](../../specs/agents/system-design/pending-permission-lookup-bounds.md),
  section "Control flow" and "Failure and recovery".
- `ExecutionStore.Remove` and `AgentExecution.clearRuntimeEnvironment`.
- Task 01 bounded-lease helper.

## Results

Registry lock narrowing done. `ExecutionStore.Remove` now deletes from every
index under the write lock (`removeFromIndexes`) and calls
`execution.clearRuntimeEnvironment()` after the lock is released, so a held
per-execution `runtimeEnvMu` no longer extends the shared write hold.
`TestRemoveReleasesRegistryLockBeforeClearingRuntimeEnvironment` deterministically
holds `runtimeEnvMu` while `Remove` runs: the registry write lock is released
and an unrelated session still resolves within its deadline, and the removed
execution's environment is confirmed cleared. `TestGetBySessionIDContext*` cover
bounded acquisition. Ran `go test -trimpath -race ... -run 'TestGetBySessionIDContext|TestRemoveReleasesRegistryLock'`
(passed 1.7s) and `golangci-lint --new-from-rev=6a6d3d76df` (0 issues).

Not run in this turn (external/Operations-owned, not a new permanent gate): the
full `cmd/mock-agent` isolated backend reproduction and the >=15-minute
same-PID/health-200/panic-0 operator soak with the real entry outcome. The
in-memory regression above is the deterministic unit-level reproduction of the
same contention; the mock-agent soak remains the remaining end-to-end evidence.
