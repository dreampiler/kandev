---
id: "01-restart-run-outcome-classification"
title: "Classify restart run settlement from the exact-turn outcome"
status: todo
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-OFFICE-AUTOMATION-CONTINUITY-003
acceptance_criteria:
  - AC-OFFICE-AUTOMATION-CONTINUITY-003.7
system_design:
  - ../../specs/office/system-design/automation-runs.md
---

# Task 01: Classify Restart Run Settlement from the Exact-Turn Outcome

## Summary

Settle an open bound automation run from the exact turn's durably recorded outcome
instead of the "not live now" observation. Record the outcome on the exact turn at
the completion site, and have restart reconciliation read it back.

## In scope

- Record `succeeded` / `failed` / `cancelled` on the exact turn via
  `PatchTurnMetadata` at `handleAutomationTurnCompleteForTurn` and the retained
  failure path, before the run settlement.
- Widen the read-only liveness seam to `Live` / `Completed(outcome)` / `Unknown`
  and classify in `ReconcileOpenRuns`.
- Add the bounded outcome metadata key beside `TurnMetaKeyErrorTerminated`.
- Narrow tests beside source.

## Out of scope

- Unit A settlement; new metrics/storage/API/enum; live-history rewrite; the
  `#234` two-origin work.

## Acceptance

- A run whose exact turn durably completed successfully is settled `succeeded`,
  not `failed`.
- A run whose exact turn durably completed failed or cancelled is settled `failed`.
- A run whose exact turn has no recorded outcome stays open and is re-evaluated on
  the next restart; a pending/deferred/uncertain delivery effect keeps it open.
- A genuinely stale bound run (no live turn, no completion) still settles `failed`.

## Verification

```bash
go test -trimpath -run '<restart classification>' ./internal/automation/... ./internal/orchestrator/...
go vet -trimpath ./internal/automation/... ./internal/orchestrator/... ./internal/task/models/...
```

Run from `apps/backend`; `gofmt -l` on changed files.

## Files likely touched

- `apps/backend/internal/orchestrator/event_handlers_automation.go`
- `apps/backend/internal/automation/service.go`
- `apps/backend/internal/task/models/models.go`
- `apps/backend/internal/automation/*_test.go` (new file beside source)
- `docs/specs/office/requirements/automation-continuity.md`
- `docs/specs/office/system-design/automation-runs.md`

## Dependencies

None. Unit A is merged.

## Risks

- Only a proven successful completion may be settled succeeded; cancellation and
  abandonment must not be.
- A read error must leave the run open, never fail-open into a settlement.

## Parallelism

`sequential`

## Inputs

- `AC-OFFICE-AUTOMATION-CONTINUITY-003.7`.
- `docs/specs/office/system-design/automation-runs.md` (exact-turn outcome section).
- `internal/automation/service.go:1036-1088`;
  `internal/orchestrator/event_handlers_automation.go:94-124,1415-1456`.

## Results

Not yet implemented. Source awaits the parent controller's explicit scoped
implementation handoff.
