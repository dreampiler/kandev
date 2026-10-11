---
created: 2026-10-11
status: draft
requirements:
  - REQ-AGENTS-PENDING-PERMISSION-LOOKUP-001
system_design:
  - ../../specs/agents/system-design/pending-permission-lookup-bounds.md
legacy_specs: []
---

# Implementation Plan: Pending Permission Lookup Bounds

## Overview

Bound the in-process acquisition path of a pending agent permission operation so
one session's cancel escalation cannot starve another session's lookup, and
remove the shared-registry write ownership that is held while per-execution
runtime state is cleared. The work ships in two ordered steps: first the bounded
context-aware acquisition across the list/resolve/cancel path, then the registry
lock narrowing that removes the escalation-held shared write window. The second
step depends on the first because its cross-session reproduction needs the
deadline path to observe a bounded failure instead of an unbounded block.

## Scope

### In scope

- One request context that covers execution lookup, agentctl-client acquisition,
  and the provider call for list, resolve, and cancel.
- A context-aware registry read lease and client read lease that fail closed
  with the context error and never start a goroutine.
- Releasing the execution-registry write ownership before per-execution runtime
  environment is cleared during execution removal.
- Source-adjacent unit tests for the changed logic.

### Out of scope

- The agentctl server-side permission response bound and delivery-writer
  cancellation. These are reused from the preceding bounded-response work
  (#222) and are not duplicated.
- The external MCP list/resolve tool contract, authorization, and audit fields.
- The permission mode and auto-approve delivery contract.
- Agent stall detection and recovery.
- Delivery retention and starting-route cleanup. This plan shares no dependency
  with them for file overlap alone.
- New permanent integration, E2E, or CI tests, monitors, heartbeats, ledgers,
  hashes, or audits.

## Technical approach

### Step 1: Bounded acquisition (Task 01)

The lifecycle permission methods
(`Manager.ListPendingPermissionsBySessionID`, `ResolvePermissionBySessionID`,
`CancelPermissionBySessionID` in
`apps/backend/internal/agent/runtime/lifecycle/manager_interaction.go`) currently
call `executionStore.GetBySessionID` and `execution.AcquireAgentCtlClient` before
deriving the 30-second provider context. Move the single
`context.WithTimeout(ctx, 30*time.Second)` to the top of each method and use it
for every step.

Add a context-aware registry read lease on `ExecutionStore`
(`apps/backend/internal/agent/runtime/lifecycle/execution_store.go`) and a
context-aware client read lease on `AgentExecution`
(`apps/backend/internal/agent/runtime/lifecycle/types.go`). Both poll
`TryRLock` through the shared bounded-lease helper extracted from
`waitForContextResetLease`
(`apps/backend/internal/agent/runtime/lifecycle/execution_context_reset.go`),
which already returns `ctx.Err()` without spawning a goroutine. Keep the
existing non-blocking `GetBySessionID` and `AcquireAgentCtlClient` for callers
that must not fail on contention.

`stopExecutionAgentctl` (`manager_interaction.go`) holds `agentctlLifecycleMu`
across `client.Stop`/`Close`; the bounded client lease makes that path degrade to
a deadline error instead of an unbounded wait for the permission operations
only. Escalation behavior itself is unchanged.

### Step 2: Narrow the registry write window (Task 02)

`ExecutionStore.Remove` (`execution_store.go`) holds the registry write lock
while calling `execution.clearRuntimeEnvironment()`, which takes the
per-execution `runtimeEnvMu`. Delete the indexes and update the runtime gauge
under the lock, capture the execution pointer, release the lock, then clear the
runtime environment outside the lock. The execution is already absent from every
index at that point, so no concurrent lookup can observe it, and a held
`runtimeEnvMu` no longer extends the registry write hold.

## Tests

| Acceptance criterion | Evidence |
| --- | --- |
| AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.1 | `manager_permission_deadline_test.go`: a blocked registry/client lease returns the deadline error within the bound. |
| AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.2 | `manager_permission_deadline_test.go`: a second session's lookup returns while another execution's removal is held on `runtimeEnvMu`. |
| AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.3 | `manager_permission_deadline_test.go`: a timed-out acquisition performs no provider call and records no audit result. |
| AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.4 | Existing permission tests (`manager_permission_test.go`, `manager_permission_acp_test.go`, orchestrator `agent_permissions_test.go`) stay green. |
| AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.5 | Existing `manager_permission_timeout_test.go` remains the reused provider bound; no new provider timer. |

Source-adjacent unit tests are the Kandev exception for changed logic. The
isolated mock-agent reproduction is a temporary, untracked harness run outside
automatic test collection; it is not added to the suite.

## E2E tests

No new Playwright test. The user-visible outcome is latency and liveness of an
existing flow, not a new rendered interaction. The package's end-to-end evidence
is the operator-run isolated merged-backend soak under Acceptance and the
temporary mock-agent reproduction. Per the request, no new permanent
integration/E2E/CI test is added.

## Acceptance (operator-run)

Owned by Operations, not a new permanent gate: run the isolated merged full
backend for at least 15 minutes with a stable PID, health 200, and zero panics,
and observe the real entry outcome (a pending permission resolved while another
session's cancel escalation runs). Operations owns runtime profiles, pprof, and
dumps; the existing AIWS W-1010-35 owns capture. No new capture task.

## Work orders

- [x] [Task 01: Bounded acquisition for pending permission operations](task-01-bounded-acquisition.md)
- [x] [Task 02: Narrow the execution-registry write window and reproduce cross-session isolation](task-02-narrow-store-lock.md)

## Verification results

Implemented on branch `feature/2baf4969-9812-486b-9-jwi` from base
`6a6d3d76df`.

- `(cd apps/backend && gofmt -l internal/agent/runtime/lifecycle/)` -> clean.
- `(cd apps/backend && go build ./...)` -> passed.
- `(cd apps/backend && go vet ./internal/agent/runtime/lifecycle/)` -> passed.
- `(cd apps/backend && go test -trimpath -race ./internal/agent/runtime/lifecycle/ -run 'TestGetBySessionIDContext|TestRemoveReleasesRegistryLock' -count=1)` -> ok, 1.7s.
- `(cd apps/backend && go test -trimpath ./internal/orchestrator/ -run 'Permission' -count=1)` -> ok, 2.1s.
- `(cd apps/backend && golangci-lint run ./internal/agent/runtime/lifecycle/... --new-from-rev=6a6d3d76df --timeout=5m)` -> 0 issues.

Not run in this turn: the full `cmd/mock-agent` isolated reproduction and the
Operations-owned >=15-minute same-PID/health-200/panic-0 soak with the real entry
outcome. Recorded as remaining evidence, not as passed.

## Risks

- Polling a `TryRLock` lease adds a small latency under contention; the poll
  interval must stay short and the deadline bound must match the existing
  provider bound.
- Removing the nested lock in `Remove` must not change the observable
  environment-clearing guarantee; a regression test must prove the environment
  is still cleared after removal.
- The cross-session reproduction depends on a deterministic hold on the
  per-execution lock; a flaky reproduction would give false confidence.
- The running agentctl at incident time lacked the lookup handler, so the
  shared-global-lock cause remains a hypothesis until the reproduction and the
  operator soak corroborate it. Operations confirmed the deployed revision was a
  deliberate pre-#3598 rollback, not an install error. The target contract is the
  current main (`6a6d3d76df`); rolled-back behavior is not this task's target, and
  no candidate redeployment or install check belongs here. Source evidence from
  the Operations causal bisect is received through the parent.

## Open questions

- Is a fixed 30-second acquisition bound acceptable, or should the acquisition
  bound be a smaller fraction of the provider bound so the provider call still
  has time to run? The plan assumes the single existing 30-second request bound
  is acceptable because acquisition is normally instantaneous.
