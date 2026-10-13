---
id: TASK-RELEASE-CONTROLLER-HANDOFF-01
title: Define checkpoint authority and durable evidence
status: pending
wave: 1
depends_on: []
plan: docs/plans/release-controller-handoff/plan.md
requirements:
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-004
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-005
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-006
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-007
acceptance_criteria:
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-004.1
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-004.2
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-005.1
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-005.2
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-006.1
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-006.2
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-007.1
  - AC-RELEASE-RELEASE-CONTROLLER-HANDOFF-007.2
system_design: docs/specs/release/system-design/release-controller-handoff.md
---

# Work Order 01: Define Checkpoint Authority and Durable Evidence

## Goal

Implement the verified persistence and transition contract for exact-scope approval, immutable evidence, distinct deploy approval and origin acknowledgment, human hold, compare-and-swap, and idempotent replay.

## Scope

Complete approval integration. The current consumer accepts JSON-only user-message records, and selection approval_ref currently checks only non-empty input; it does not validate that the existing human approval covers the exact approved_leaf_ids. A natural-language reuse patch was rejected by automatic approval review because lexical substring matching overmatched scope. Do not claim approval reuse is complete. Do not introduce a new authorization JSON format.

## Acceptance

- After integration, validate existing approval_ref and deploy_approval_ref records against exact scope before writes.
- Review, cleanup, test, deploy approval, deployment, verification, origin acknowledgment, and close each have separately readable evidence.
- Evidence is read from the authoritative immutable source for the exact approved revision and target.
- Stale versions are rejected and replay does not duplicate side effects.
- Uncertain side effects require authoritative readback before retry.
- Missing, stale, conflicting, or non-authoritative evidence retains human hold.
- Tests cover unauthorized scope, exact approval-reference validation, stale writes, replay, and uncertain-result recovery.
- Shipment remains blocked until exact selection scope validation is integrated and verified.

## Out of scope

Bulk changes, scheduler bypass, deployment without approval, close without origin acknowledgment, and operating configuration changes.