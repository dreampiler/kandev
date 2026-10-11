---
status: draft
system: agents
created: 2026-10-11
owners:
  - kandev
---

# Pending Permission Lookup Bounds Requirements

## Overview

Kandev exposes pending agent permission requests through an in-process path:
an authorized caller (the external MCP list/resolve/cancel surface, the web
session UI, or the idle reaper's automatic "has known work" check) resolves a
task session to its tracked agent execution, acquires the live agentctl client,
and only then issues the provider call. Today the provider call has a bounded
wait, but the steps before it do not: execution lookup and client acquisition
take an unbounded in-process lock. A single session whose agent is being
cancelled, stopped, or locally escalated can therefore hold shared runtime state
long enough that an unrelated session's pending-permission lookup never reaches
its own deadline. The user-visible result is a permission that appears
permanently pending (or a stale `permission_not_found`) until a restart, and an
idle session that is never parked because the automatic check never returns.

The agents system owns this contract because pending agent permission state, the
agent execution registry, and the live agentctl client are agent-runtime
concepts. Task storage and the external MCP surface consume the outcome without
owning its liveness.

## Terminology

- **Pending permission operation:** one list, resolve, or cancel call for a
  pending agent permission request of one task session.
- **Acquisition:** the execution-store lookup and agentctl-client acquisition
  that happen before the provider call.
- **Caller deadline:** the context deadline the caller supplies to the pending
  permission operation.
- **Cancel escalation:** the lifecycle behavior that releases a locally stuck
  in-flight prompt after a cancel is not acknowledged.

## Requirements

### REQ-AGENTS-PENDING-PERMISSION-LOOKUP-001: Pending permission operations respect the caller deadline

**Intent:** A pending permission operation for one task session must complete or
fail within its caller deadline even while another task session is cancelling,
stopping, or escalating an agent. It must never block indefinitely on
in-process lock acquisition, and a timeout must not leave a background task that
later answers the provider.

#### Acceptance criteria

- **AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.1:** GIVEN an authorized caller and
  a caller deadline, WHEN it lists, resolves, or cancels a pending permission
  for a task session, THEN execution lookup, agentctl-client acquisition, and
  the provider call all occur under that one deadline and the operation returns
  a deadline error instead of blocking past it.
- **AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.2:** GIVEN one task session is
  cancelling, stopping, or locally escalating an agent while a different task
  session's pending permission operation runs, WHEN the two contend for shared
  runtime state, THEN the second operation still returns within its deadline.
- **AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.3:** WHEN a pending permission
  operation cannot acquire the runtime state it needs before its deadline, THEN
  it returns without recording a permission audit result, without answering the
  provider, and without leaving a background task that later answers the
  provider or records a result.
- **AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.4:** WHEN a pending permission
  operation completes within its deadline, THEN today's outcome is preserved:
  exactly one provider option is delivered per resolution, the live request is
  consumed once, and the agent continues from the same conversation.
- **AC-AGENTS-PENDING-PERMISSION-LOOKUP-001.5:** A pending permission operation
  does not add a second provider response wait that competes with the existing
  bounded agent response wait.

## Out of scope

- The agentctl server-side permission response bound and delivery-writer
  cancellation. Those are delivered by the preceding bounded-response work and
  are reused, not changed.
- The external MCP list/resolve tool contract, its authorization rules, and its
  audit fields. Those remain owned by
  [External Agent Permission Resolution](external-permission-resolution.md);
  this requirement only constrains their liveness and latency.
- The permission mode and auto-approve delivery contract. That remains owned by
  [Agent Permission Control Integrity](permission-control-integrity.md).
- Agent stall detection and recovery, which remains owned by
  [Agent Stall Recovery](agent-stall-recovery.md).
- Delivery retention and starting-route cleanup.
- Changing which provider commands require a permission.
