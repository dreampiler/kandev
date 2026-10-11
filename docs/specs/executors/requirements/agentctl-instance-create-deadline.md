---
status: draft
system: executors
created: 2026-10-12
owners:
  - kandev
---

# Agentctl instance creation deadline

## Overview

The backend creates an agent instance through the agentctl control API before
an agent can start. Instance creation does more work than other control calls:
the controller starts the instance's process manager and workspace trackers
before it responds. Under concurrent session starts, that response can take
longer than the deadline used for short control calls.

This contract gives instance creation its own bounded client deadline. Other
control calls keep their existing deadline.

Executors owns this contract because it defines how execution environments
reach their controller.

## Terminology

- **Control call:** A backend request to the agentctl control API, such as
  instance creation, instance deletion, health, or diagnostics.
- **Creation deadline:** The longest time the backend waits for a controller
  response to one instance creation request.

## Requirements

### REQ-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001: Bounded instance creation wait

**Intent:** A controller that is slow but still progressing can finish
creating an instance, while the backend never waits for creation without a
bound.

#### Acceptance criteria

- **AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.1:** When the backend sends an
  instance creation request, the system shall wait up to 120 seconds for the
  controller response before it fails the request.
- **AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.2:** Control calls other than
  instance creation shall keep their existing 30-second deadline.
- **AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.3:** When the caller cancels
  instance creation or its own deadline expires first, the system shall stop
  waiting at that point and report the creation failure to the caller.
- **AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.4:** Instance creation shall use
  the same authentication credential and runtime binding as other control calls
  from the same client.

## Out of scope

- How the controller serializes concurrent instance creation, and how it
  cleans up a creation whose caller already gave up.
- Creation that takes longer than the creation deadline. Such a request still
  fails with a timeout.
- Retry, backoff, or failure classification for instance creation.
- Deadlines for agent launch phases outside the control call.
- A user or operator setting for the creation deadline.

## Related design

- [Agentctl instance creation deadline](../system-design/agentctl-instance-create-deadline.md)
- [Agentctl authentication modes](agentctl-authentication-modes.md)
