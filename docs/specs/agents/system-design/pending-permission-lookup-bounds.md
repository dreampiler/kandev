---
status: draft
system: agents
requirements:
  - REQ-AGENTS-PENDING-PERMISSION-LOOKUP-001
---

# Pending Permission Lookup Bounds System Design

## Purpose and boundaries

This design bounds the in-process work that precedes the provider call for a
pending agent permission operation, so one session's cancel escalation cannot
starve another session's lookup.

The agents system owns the runtime execution registry and the live agentctl
client, so this design owns the acquisition path. Adjacent contracts that this
design uses but does not own:

- [External Agent Permission Resolution](../requirements/external-permission-resolution.md)
  owns the list/resolve tool contract, authorization, and audit fields.
- [Agent Permission Control Integrity](../requirements/permission-control-integrity.md)
  owns option selection and auto-approval.
- The agentctl bounded permission response
  (`internal/agentctl/server/adapter/transport/shared.Config.PermissionTimeout`,
  `DefaultPermissionTimeout`) is delivered by the preceding bounded-response
  work and is reused, not replaced.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-PENDING-PERMISSION-LOOKUP-001` | [Control flow](#control-flow) |

## Components and responsibilities

- **Orchestrator permission service** (`orchestrator.Service.ListPendingAgentPermissions`,
  `ResolveAgentPermission`, `cancelAgentPermission`) authorizes the task/session
  pair and fans a task-wide list over sessions. It passes the caller context
  through unchanged and adds no deadline of its own.
- **Executor adapter** (`executor.Executor.ListPendingPermissions`,
  `ResolvePermission`, `CancelPermission`) maps `ErrNoExecutionForSession` to its
  own sentinel and forwards the context.
- **Lifecycle manager permission methods**
  (`lifecycle.Manager.ListPendingPermissionsBySessionID`,
  `ResolvePermissionBySessionID`, `CancelPermissionBySessionID`) own the
  bounded acquisition. Each derives one request context and uses it for lookup,
  client acquisition, and the provider call.
- **Execution registry** (`lifecycle.ExecutionStore`) owns the session index.
  Its `GetBySessionID` read and `Remove` write are the shared contention point
  across sessions.
- **Execution** (`lifecycle.AgentExecution`) owns the live agentctl client and
  the per-execution environment. `AcquireAgentCtlClient` takes the client read
  lease; `clearRuntimeEnvironment` takes the per-execution environment write
  lock.
- **Automatic idle check** (`orchestrator.Service.sessionHasKnownIdleWork`)
  calls the list method and is a consumer of this bound; it does not get a
  separate path.

## Data and contracts

- **Bounded lease helper.** The repository already bounds lock acquisition
  without a goroutine in `waitForContextResetLease` (`execution_context_reset.go`):
  it polls a `TryLock`/`TryRLock` function and selects on the context, returning
  `ctx.Err()`. This design reuses that shape for the registry read lease and the
  agentctl-client read lease. Extracting the poll loop into one shared helper is
  preferred over a second copy.
- **One request context per operation.** `ListPendingPermissionsBySessionID`,
  `ResolvePermissionBySessionID`, and `CancelPermissionBySessionID` derive a
  single request context once (`context.WithTimeout(ctx, 30*time.Second)`, the
  existing provider bound) and use it for every step, including acquisition.
  The provider call keeps its current bound and does not gain a second timer.
- **Deadline error.** A timed-out acquisition returns `context.DeadlineExceeded`
  wrapped with the session identity, so callers classify it as a transient
  deadline failure rather than `permission_not_found`.
- **Registry read lease.** `ExecutionStore` exposes a context-aware session
  lookup that fails closed with the context error when it cannot take the read
  lease within the deadline. The existing `GetBySessionID` stays for
  non-blocking callers that must not fail on contention.

## Control flow

1. Caller invokes a pending permission operation with a deadline context.
2. The lifecycle method derives one request context from the caller context and
   the existing provider bound.
3. It takes the execution-registry read lease with the bounded helper. On
   timeout it returns the deadline error; it does not start any other work.
4. It takes the agentctl-client read lease with the bounded helper. On timeout it
   releases the registry lease and returns the deadline error.
5. It issues the provider list/resolve/cancel call under the same request
   context.
6. The lease release functions run before the provider response is returned to
   the caller.

The provider response is the only side effect. Because the operation does not
start a goroutine to acquire a lease, a caller timeout cannot leave a background
task that later answers or records a permission.

## Failure and recovery

- **Acquisition timeout:** return the deadline error; no provider call and no
  audit write. The request stays pending and answerable.
- **Provider timeout:** unchanged from today; the existing bounded response
  handling applies.
- **No abandoned work:** the bounded helper is a synchronous poll. It does not
  spawn a goroutine, so no late mutation can occur after the caller's timeout.
- **Cancel escalation isolation:** the fix removes the shared-lock wait from the
  escalation window rather than changing escalation itself. A cancel that
  escalates still releases its own execution and publishes its ready event as
  today; it simply no longer holds the registry write ownership while it waits on
  per-execution environment state.

## Persistence

No change. Pending permission liveness remains in the live agentctl process;
permission audit rows persist through the existing task-session message path and
are untouched by this design. All fields here are in-memory runtime state.

## Security

No change. The task/session authorization in the orchestrator remains the only
gate before runtime state is read; this design adds no new read surface and
returns no provider or credential data on a deadline error. Deadline errors must
not distinguish an authorized session from an unknown one beyond what the
existing not-found contract already allows.

## Observability

A deadline-exceeded acquisition emits one structured `warn` log carrying the
execution id, session id, and the acquisition stage that timed out. No new
metric is added; the metric identity sets in the backend are closed, and adding
one would require a separate approved change. The log is the operator evidence
for the isolated reproduction and soak described in the plan.

## Related decisions

- [ADR-2026-08-09-exclusive-runtime-state-ownership](../../../decisions/2026-08-09-exclusive-runtime-state-ownership.md)
- [ADR-2026-08-11-live-agent-permission-authority](../../../decisions/2026-08-11-live-agent-permission-authority.md)
- [ADR-2026-09-28-attached-idle-runtime-parking](../../../decisions/2026-09-28-attached-idle-runtime-parking.md)
