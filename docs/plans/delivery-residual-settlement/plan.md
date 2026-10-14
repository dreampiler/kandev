---
created: 2026-10-13
status: draft
requirements:
  - REQ-PLATFORM-DELIVERY-RESIDUAL-001
  - REQ-PLATFORM-DELIVERY-RESIDUAL-002
  - REQ-PLATFORM-DELIVERY-RESIDUAL-003
system_design:
  - ../../specs/platform/system-design/delivery-residual-settlement.md
legacy_specs: []
---

# Plan: delivery residual settlement (design checkpoint)

Status: implemented in the second PR under explicit parent implementation order.
Task 02's workflow-bound classification is implemented in the third PR from
durable turn and step facts (no engine evaluation).

## Work orders

- `task-01-dead-owner-submission-settlement.md`: settle active-state
  submissions with terminal/missing owners via the existing settlement path.
- `task-02-block-resolved-workflow-completion.md`: no-op workflow receipts via
  the Task 04 writer + existing `block_resolved` promotion.

## Validation (when implemented)

- Companion backend unit tests beside the settlement source (same package).
- Narrow `go test -run` on the touched package; `go vet`; lint new-from-rev.
- Docs validation: `python3 scripts/list-docs.py validate`,
  `python3 scripts/lint-spec-files.py --all`.
- No new integration/E2E/CI/collection paths. No production DB execution.

## Remaining operator readback

- Replaced-binary one-hour natural inflow/projection/settlement readback
  (same procedure as completed-stream Task 02).
