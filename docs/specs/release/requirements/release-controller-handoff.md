---
title: Release Controller Handoff Requirements
status: draft
system: release
created: 2026-10-01
owners:
  - release
---

# Release Controller Handoff Requirements

## Purpose and scope

Draft requirements for a release controller whose code is under integration. It moves only explicitly approved leaf work through release checkpoints and preserves native wait/launch rules. It does not take ownership of parent or sibling controllers, mutate unrelated tasks, or perform broad operating changes. The feature is not yet available for operation.

## Requirements

### REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-001: Approved leaf scope

The target behavior is to act only on leaf identifiers covered by an existing human approval record, using its reference rather than a new authorization JSON format. Current approval_ref scope validation is not integrated; this requirement is not yet satisfied. Never infer authority from a parent, sibling, message, project membership, or broad status query.

#### Acceptance criteria

- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-001.1:** Every write is limited to exact approved identifiers and declared actions.
- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-001.2:** Parent, sibling, and unlisted leaves cannot be changed.

### REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-002: Native wait and queue preservation

Respect native input-wait and launch-block decisions. Preserve blocked state and queue order; do not bypass, dequeue, promote, or fabricate a launch.

#### Acceptance criteria

- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-002.1:** A wait/block leaves item and queue order intact and records the observed reason.
- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-002.2:** Resume only after fresh native state permits action.

### REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-003: Ordered all-success members

Run approved group members in declared order. Advance only after each required member succeeds and its slot/worktree return is read back. Failure, missing evidence, ambiguity, or stale results retain the hold.

#### Acceptance criteria

- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-003.1:** No later member starts before success evidence and slot/worktree return are confirmed.
- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-003.2:** Any unsuccessful or unverifiable member prevents advancement.

### REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-004: Distinct release checkpoints

Enforce review, cleanup, test, explicit deploy approval, deploy, verification, explicit origin acknowledgment, then close. Deploy approval and origin acknowledgment are separate decisions. Reuse the existing human deployment approval reference; a boolean by itself is not authority.

#### Acceptance criteria

- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-004.1:** Deploy requires validation of the existing approval reference for the exact release inputs.
- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-004.2:** Close requires successful verification and explicit acknowledgment by the trusted origin.

### REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-005: Authoritative evidence

Support each checkpoint with readback of persisted evidence for the exact release. Mutable task text or local memory alone is insufficient.

#### Acceptance criteria

- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-005.1:** Record evidence references and timestamps for each checkpoint.
- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-005.2:** Missing, conflicting, or unverified evidence retains the human hold.

### REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-006: Restart safety

Use compare-and-swap or equivalent version preconditions. Replays must not duplicate side effects. Read back authoritative state before retrying uncertain side effects.

#### Acceptance criteria

- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-006.1:** Stale state cannot overwrite a newer transition.
- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-006.2:** Replayed requests do not duplicate logical transitions or deployment.

### REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-007: Human hold and bounded operation

Failures, ambiguity, absent evidence, and native waits retain a visible human hold. No bulk operating changes or mutation outside the approved leaf set.

#### Acceptance criteria

- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-007.1:** Hold reason remains visible until an authorized decision resolves it.
- **AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-007.2:** Every write traces to an approved leaf and declared checkpoint.

## Implementation detail status

The REST/MCP names and selected fields are recorded in the system design. Exact approval-reference scope validation is not integrated; shipment and operational use are blocked.