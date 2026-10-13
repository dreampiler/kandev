---
title: Release Controller Handoff System Design
status: draft
system: release
requirements:
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-001
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-002
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-003
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-004
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-005
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-006
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-007
---

# Release Controller Handoff System Design

## Status

Draft design. Controller code has been written and is being integrated; end-to-end behavior and operational availability are not yet verified.

## Interface reported for this draft

The code currently registers REST POST /tasks/:id/release-handoff (origin task ID), GET /tasks/:id/release-handoff, POST /tasks/:id/release-handoff/advance, and POST /tasks/:id/release-handoff/ack (release task ID for the latter operations). MCP tools are begin_release_handoff_kandev, get_release_handoff_kandev, advance_release_handoff_kandev, and ack_release_handoff_kandev.

The reported begin request fields are intent_id, project_id, release_workflow_id, release_step_id, wait_step_id, approved_leaf_ids, approval_ref, deploy_approval_ref, deploy_approved. Advance reports expected_version, phase, evidence with receipt_ref, actor_id, kind (background-run), and outcome (succeeded); its DTO also carries deploy_approval_ref for the existing deployment approval. The reported receipt form is background-run:<sessionID>:<runID>, backed by persisted DB stdout structured-stage proof. Acknowledgment is reported to require trusted origin, release ID, and expected version.

Deployment approval can reuse an existing JSON user-message approval reference: the advance path validates deploy_approval_ref, creates a compare-and-swap grant, and returns the updated expected_version so the same release can resume without a new approval conversation. Selection approval remains incomplete: approval_ref currently receives a non-empty check only, without validation that the referenced record covers the exact leaves. A natural-language reuse patch was rejected by automatic approval review because lexical substring matching overmatched the requested scope. MCP acknowledgment derives origin from the trusted task-bound caller; runtime operation is not applied.
## Known integration blockers

- Deployment approval reuses an existing deploy_approval_ref: the advance path validates the reference, creates a CAS grant, and returns the updated expected_version so the same release resumes without a new approval conversation.
- A natural-language selection-approval reuse patch was rejected by automatic approval review because lexical substring matching overmatched the requested scope.
- The selection approval reference is not yet validated against its exact leaf scope.
- A-to-verification-B evidence mismatch was fixed in the integration code. ReleaseEvidence.Deployment persists the server-verified deployment proof snapshot; caller-supplied snapshot claims are not trusted. Snapshot-fix tests are still pending; no E2E pass is claimed for this fix.
- Do not ship. Runtime operation and home deployment are not applied.

## Context and boundaries

A sibling controller coordinates only an explicitly approved leaf set. It does not take ownership of parent or sibling controllers. The platform remains authoritative for native wait/launch behavior and queue order. The intended approval contract binds exact leaves, revision, artifact, target, and allowed actions; current scope enforcement remains a blocker.

## Conceptual components

- Approval integration (blocked): current consumer accepts JSON-only user-message records; approval_ref non-empty validation exists, but binding it to the exact approved leaf scope is not integrated. Do not represent existing approval reuse as complete.
- Checkpoint coordinator (TBD): advances a leaf through named release checkpoints and stores evidence references.
- Native scheduler adapter (TBD): reads eligibility and queue order; never overrides a native block.
- Evidence reader (TBD): reads authoritative immutable review, cleanup, test, deployment, and verification records.
- Origin acknowledgment gate (TBD): requires explicit acknowledgment before close.

These are conceptual roles only.

## State and transitions

Conceptual progression: approved → review → cleanup → test → deploy-approved → deployed → verified → origin-acknowledged → closed. Any failure, absent/conflicting evidence, stale version, or native wait enters human-hold without losing the last confirmed checkpoint or queue position.

Deployment approval and origin acknowledgment are distinct transitions. Close requires both verification and origin acknowledgment.

## Safety and concurrency

Condition each transition on its last observed version (compare-and-swap or equivalent). Assign idempotency identity to each transition. After interruption or uncertain side effect, read the authoritative immutable record before retry.

For ordered groups, do not start the next member until prior success evidence and slot/worktree return are read back. On native wait or launch block, preserve queue position and wait; never manually dequeue or synthesize a launch.

## Evidence and failure behavior

Evidence identifies the exact leaf and release inputs, authoritative source, timestamp, result, and immutable reference. Mutable comments, task descriptions, process completion, or controller-local state alone do not prove a checkpoint.

Failures and ambiguities fail closed into human hold. Recovery starts with fresh native state and immutable evidence readback. Do not guess whether an external side effect occurred or alter operating configuration to clear a hold.

## Operational constraints

- No broad operating changes or bulk mutations.
- No native scheduler bypass or queue edits.
- No deployment without exact-target explicit approval.
- No close without verification and explicit origin acknowledgment.
- Route and tool names above are present in the integration branch. End-to-end behavior, operating availability, and any further schema details remain under verification.