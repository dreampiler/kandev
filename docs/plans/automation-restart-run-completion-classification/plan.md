---
created: 2026-10-12
status: draft
requirements:
  - REQ-OFFICE-AUTOMATION-CONTINUITY-003
system_design:
  - ../../specs/office/system-design/automation-runs.md
legacy_specs: []
---

# Implementation Plan: Restart terminal-run outcome classification

## Overview

A scheduled automation run that reaches an open (`triggered` / `task_created`)
state is settled by `ReconcileOpenRuns` on the next restart. Today it settles the
run `failed` whenever the exact bound turn is not live, without asking whether the
turn actually completed. A session parked after a completed turn (`WAITING_FOR_INPUT`)
is not live, so `AutomationRunLive` returns false and an already completed run is
rewritten `failed`. This plan settles an open bound run from the exact turn's
durably recorded outcome instead.

Source: `internal/automation/service.go:1036-1088` (`ReconcileOpenRuns`),
`internal/orchestrator/event_handlers_automation.go:94-124` (`AutomationRunLive`),
`:1415-1456` (`handleAutomationTurnCompleteForTurn`),
`internal/task/repository/sqlite/session.go:233-239` (`GetTurn`).

## Status

Source implemented on this task's worktree; owner review is the PR. The owning
clauses (`AC-OFFICE-AUTOMATION-CONTINUITY-003.7` and the system-design section) are
written before the source, per the plan/work-order checkpoint.

## Scope

### In scope

- Record the exact turn's outcome (`succeeded` / `failed` / `cancelled`) at the
  single completion site, before the run settlement, using the existing turn
  metadata seam (`TurnService.PatchTurnMetadata`).
- Classify an open bound run from that exact-turn outcome in `ReconcileOpenRuns`.
- Preserve genuine stale failures, live sessions, and the unit A guards.

### Out of scope

- Unit A (failed-run generated-task settlement), which is merged.
- New metrics, storage, API, run statuses/enums, or a recurring sweeper.
- Any live-history rewrite.

## Technical approach

Reuse the existing exact-turn metadata seam:

- `handleAutomationTurnCompleteForTurn`
  (`internal/orchestrator/event_handlers_automation.go:1415-1456`) already computes
  `success := !isError && stopReason != agentEventError && !cancelled`. Persist the
  matching outcome on the exact turn via `PatchTurnMetadata` **before**
  `markAutomationRunTerminalForTurn` (`:1450`). `PatchTurnMetadata` merges into a
  completed turn (`internal/task/repository/interface.go:584`). The retained-failure
  path (`finalizeAutomationRun`) records the failure outcome the same way.
- `ReconcileOpenRuns` (`internal/automation/service.go:1036-1088`) reads the exact
  turn's outcome through the existing read-only liveness seam
  (`RunLivenessChecker`, `internal/automation/service.go:85-88`), widened to
  `Live` / `Completed(outcome)` / `Unknown`:
  - `succeeded` → settle `succeeded`;
  - `failed` / `cancelled` → settle `failed`;
  - `Live` → leave open;
  - `Unknown` (no recorded outcome, or a read error) → leave open and log once per
    startup pass; the next restart re-evaluates. A pending/deferred/uncertain
    delivery effect keeps the run open.

The seam matches the run's task/session/turn identity against the turn's own
binding, treats existing authoritative negative evidence (`error_terminated` /
`interrupted`) as failure, and never overwrites a recorded outcome. The
managed-conversation and webhook protections and the unbound path keep their
existing order ahead of any run settlement.

A new bounded turn-metadata key constant lives beside
`models.TurnMetaKeyErrorTerminated` (`internal/task/models/models.go:836-842`).

## Work orders

- [ ] [Task 01: Classify restart run settlement from the exact-turn outcome](task-01-restart-run-outcome-classification.md)

## Verification

- `go test -trimpath -run '<restart classification>' ./internal/automation/... ./internal/orchestrator/...`
- `go vet` over the changed packages; `gofmt -l` on changed files.
- `python3 scripts/list-docs.py validate` and `python3 scripts/lint-spec-files.py --all`.

## Risks

- A cancelled or abandoned turn must not be recorded `succeeded`; only a turn the
  completion handler proves successful is settled succeeded.
- A read failure must never fail-open into a run settlement.
- The change must not touch unit A's settlement or any live history.
