---
id: "01-bounded-acquisition"
title: "Bounded acquisition for pending permission operations"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-PENDING-PERMISSION-LOOKUP-001
acceptance_criteria:
  - AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.1
  - AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.3
  - AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.5
system_design:
  - ../../specs/agents/system-design/pending-permission-lookup-bounds.md
---

# Task 01: Bounded acquisition for pending permission operations

## Summary

Make one request context cover the entire pending permission operation, so
execution lookup and agentctl-client acquisition are bounded by the caller
deadline and the provider call does not start after a timed-out acquisition.

## In scope

- Derive a single request context at the top of
  `Manager.ListPendingPermissionsBySessionID`,
  `Manager.ResolvePermissionBySessionID`, and
  `Manager.CancelPermissionBySessionID`.
- Add a context-aware registry read lease on `ExecutionStore` and a
  context-aware client read lease on `AgentExecution`, both built on the shared
  bounded-lease poll helper and both failing closed with the context error.
- Reuse the existing bounded-lease poll pattern already implemented by
  `waitForContextResetLease`; extract one shared helper rather than adding a
  second copy of the poll loop.
- Add a unit test proving a blocked lease returns the deadline error within the
  bound and performs no provider call.

## Out of scope

- The agentctl server-side response bound and delivery-writer cancellation
  (#222). Reuse them; do not duplicate.
- Changing `stopExecutionAgentctl` escalation semantics.
- Changing the external MCP tool contract, authorization, or audit fields.

## Acceptance

- A pending permission list/resolve/cancel with a caller deadline returns within
  that deadline when the registry or client read lease is blocked.
- A timed-out acquisition returns a deadline error and performs no provider call
  and no audit write.
- No goroutine is started to acquire a lease, so no late provider call can occur
  after the caller times out.

## Verification

```bash
(cd apps/backend && gofmt -l internal/agent/runtime/lifecycle/)
(cd apps/backend && go build ./...)
(cd apps/backend && go test -trimpath -race ./internal/agent/runtime/lifecycle/ -run 'TestGetBySessionIDContext|TestRemoveReleasesRegistryLock' -count=1)
(cd apps/backend && go test -trimpath ./internal/orchestrator/... -run 'Permission' -count=1)
```

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/manager_interaction.go`
- `apps/backend/internal/agent/runtime/lifecycle/execution_store.go`
- `apps/backend/internal/agent/runtime/lifecycle/types.go`
- `apps/backend/internal/agent/runtime/lifecycle/execution_context_reset.go`
- `apps/backend/internal/agent/runtime/lifecycle/manager_permission_deadline_test.go` (new)

## Dependencies

None. Precedes Task 02.

## Risks

- A new lock-lease helper must not change the behavior of the existing
  `waitForContextResetLease` callers.
- The deadline error must remain distinguishable from `permission_not_found` so
  callers do not expire an answerable request.

## Parallelism

`sequential`

## Inputs

- [Pending Permission Lookup Bounds](../../specs/agents/system-design/pending-permission-lookup-bounds.md),
  sections "Data and contracts" and "Control flow".
- Existing bounded-lease pattern in
  `apps/backend/internal/agent/runtime/lifecycle/execution_context_reset.go`.
- Preceding bounded-response work (#222).

## Results

Done. `ListPendingPermissionsBySessionID`, `ResolvePermissionBySessionID`, and
`CancelPermissionBySessionID` now derive one `requestCtx`
(`pendingPermissionRequestTimeout`, 30s) and use it for execution lookup
(`ExecutionStore.GetBySessionIDContext`), client acquisition
(`AgentExecution.AcquireAgentCtlClientContext`), and the provider call. Both
context-aware leases poll `TryRLock` through the shared `waitForLease` helper
(extracted from `waitForContextResetLease`), which returns `ctx.Err()` and never
starts a goroutine. `go build ./...` passed; `gofmt -l` and `go vet` clean;
`go test -trimpath -race ... -run 'TestGetBySessionIDContext|TestRemoveReleasesRegistryLock'`
passed (1.7s); `go test -trimpath ./internal/orchestrator/ -run 'Permission'`
passed (2.1s); `golangci-lint run --new-from-rev=6a6d3d76df` reported 0 issues.
