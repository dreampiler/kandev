---
title: Release Controller Handoff
created: 2026-10-01
status: draft
requirements:
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-001
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-002
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-003
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-004
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-005
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-006
  - REQ-RELEASE-RELEASE-CONTROLLER-HANDOFF-007
system_design: docs/specs/release/system-design/release-controller-handoff.md
legacy_specs: []
---

# Release Controller Handoff Plan

## Goal

Implement an auditable controller for exact approved leaf work, preserving native scheduling and human holds.

## Status and constraints

Draft only. Shipment is blocked. Code is under integration; no operational use or home deployment is applied. Do not claim availability or enabled status. No broad operating changes, native wait/launch bypass, manual queue edits, or implicit deployment authorization.

## Work order sequence

1. task-01-checkpoint-authority-and-evidence.md - resolve approval-reference scope validation and persist verified checkpoint/deployment evidence with compare-and-swap and idempotency.
2. task-02-approved-leaf-orchestration.md - enforce exact leaf scope, native wait/queue preservation, ordered all-success members, and slot/worktree return after task 01 unblocks shipment.

## Reported implementation interface

The implementation owner supplied REST/MCP names and request fields; code is being integrated. Approval scope enforcement and deployment evidence remain blockers described below.
## Known integration blockers

- The root approval consumer currently accepts JSON-only user-message approval records. A natural-language reuse patch was rejected by automatic approval review because lexical substring matching overmatched the requested scope.
- Selection approval_ref currently receives a non-empty check only. Validation that the referenced existing human approval covers the exact approved_leaf_ids is not integrated. This is a shipment blocker; do not describe approval-record reuse as complete.
- Deployment approval can reuse deploy_approval_ref through validation and CAS grant; the updated expected_version resumes the same release without a new approval conversation.
- Selection approval remains blocked: approval_ref is checked only for non-empty input, not validated against exact approved_leaf_ids. Natural-language reuse was rejected by automatic approval review after lexical substring matching overmatched scope.
- A-to-verification-B evidence mismatch was fixed in integration code. ReleaseEvidence.Deployment persists the server-verified deployment proof snapshot and does not trust caller-supplied snapshot claims. Snapshot-fix tests are still pending; no E2E pass is claimed for this fix.
- Runtime operation and home deployment have not been applied.
## Completion criteria

- Exact approved leaf scope is enforced at every write.
- Ordered members require success and slot/worktree-return readback.
- Review, cleanup, test, deploy approval, deploy, verify, origin acknowledgment, and close are distinct evidenced checkpoints.
- Stale writes/replays are safe; uncertain outcomes resolve through authoritative readback.
- Native wait decisions and queue order remain unchanged.
- Failure or ambiguity retains human hold.

Before implementation starts, record verified APIs, MCP tools, persistence fields, events, and test commands in the design/work orders; never invent names.