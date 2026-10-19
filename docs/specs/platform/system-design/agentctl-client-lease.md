---
status: current
system: platform
requirements:
  - REQ-PLATFORM-AGENTCTL-CLIENT-LEASE-001
created: 2026-10-20
owners:
  - kandev
---

# Agentctl Client Lease Design

## Boundary and mapping

| Criteria | Implementation boundary |
| -------- | ----------------------- |
| .1, .3   | `lifecycle.AgentExecution.AgentctlURL` in `agent/runtime/lifecycle/types.go` |
| .2       | `lifecycle.Manager.restoreRecoveredFailedExecution` in `manager_interaction.go` |

## Lock order

`agentctlLifecycleMu` is the client lease: bounded client operations hold its
read side, and terminal stop holds its write side while closing and detaching
the client. Go read locks are not reentrant once a writer is waiting, so a
nested read lease blocks until the outer one is released.

Execution persistence (`buildRunningFromExecution`) runs under the execution
store lock and reads `AgentctlURL`. That read therefore never takes the lease:
it loads the atomic override first and otherwise reads the construction-time
client under `agentctlSourceMu.TryRLock`, returning "" when the source lock is
busy. The base URL is immutable per client, so no lease is needed to keep it
valid.

`restoreRecoveredFailedExecution` holds the lease only for `GetStatus` and
releases it before `markBootReadyFromFailed`, so the lease is never held across
the store lock.

## Verification

Lifecycle unit tests hold a read lease, queue a writer, and require
`AgentctlURL` to return the client URL without waiting, plus the override and
empty-client cases. No process is started.
