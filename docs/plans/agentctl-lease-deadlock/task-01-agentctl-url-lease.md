---
id: "01-agentctl-url-lease"
title: "Keep agentctl URL reads off the client lease"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-AGENTCTL-CLIENT-LEASE-001
acceptance_criteria:
  - AC-PLATFORM-AGENTCTL-CLIENT-LEASE-001.1
  - AC-PLATFORM-AGENTCTL-CLIENT-LEASE-001.2
  - AC-PLATFORM-AGENTCTL-CLIENT-LEASE-001.3
system_design:
  - ../../specs/platform/system-design/agentctl-client-lease.md
---

# Task 01: Keep agentctl URL reads off the client lease

## Summary

Make `AgentExecution.AgentctlURL` a non-blocking snapshot that never takes the
agentctl client lease, and release the lease in
`restoreRecoveredFailedExecution` before the execution is marked ready.

## In scope

- `AgentctlURL`: nil-safe; atomic override first, then the source client under
  `TryRLock`; "" when no client is set or the source lock is busy.
- `restoreRecoveredFailedExecution`: release the lease after `GetStatus` and
  on the nil-client path, not by `defer`.

## Out of scope

Lease handling in other call sites, the terminal stop path, client replacement,
and detach.

## Verification

- `go test ./internal/agent/runtime/lifecycle/ -run '^TestAgentctlURL' -count=1`
  (pure unit test, no process is started)
