---
id: "02-terminal-teardown"
title: "Record and tear down a terminally-stalled execution"
status: done
wave: 2
depends_on: ["01-terminal-classification"]
plan: "plan.md"
requirements:
  - REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001
acceptance_criteria:
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.2
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.5
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.6
system_design:
  - ../../specs/agents/system-design/agent-stall-inactivity-teardown.md
---

# Task 02: Record and tear down a terminally-stalled execution

## Summary

The orchestrator handles the terminal stall classification: after the same
ownership guards the never-started path uses, it records the terminal outcome so
the session leaves `RUNNING`/`STARTING`, then requests a forced, bounded teardown.
The ceiling slot returns through the existing reservation accounting.

## In scope

- In `handleAgentStalled`
  (`apps/backend/internal/orchestrator/event_handlers_stall.go`), add a branch for
  `payload.ProlongedStall`. Reuse the ownership guards already in place.
- Record the terminal outcome first, then request teardown through the existing
  forced, bounded seam `recordAndStopStalledExecution` / `stopStalledExecution`,
  shared with the never-started classification.
- Carry the prolonged case's own error, stop reason, and notice text so the two
  classifications stay distinguishable.
- Ensure the recorded settlement moves the session out of `RUNNING`/`STARTING` so
  the ceiling reservation is released.

## Out of scope

- The lifecycle classification (Task 01).
- Advancing the workflow step or applying a `step_complete_kandev` signal the agent
  did not request.
- Changing the never-started branch or its notice copy.

## Acceptance

1. The classification records the terminal outcome and issues exactly one forced
   stop for the payload's execution.
2. A failed teardown keeps the recorded terminal state authoritative and leaves the
   execution registered.
3. The session leaves `RUNNING`/`STARTING`, so with the ceiling enabled its slot is
   returned and a deferred automatic launch becomes eligible.
4. No workflow step transition or completion signal is applied.

## Verification

```sh
cd apps/backend && go test ./internal/orchestrator/... -count=1
```

Regressions live in
`apps/backend/internal/orchestrator/event_handlers_stall_prolonged_test.go`:

- The classification records the outcome and issues one forced stop (AC-001.2).
- A teardown failure leaves the session terminal with its message.
- No step transition or completion signal is applied (AC-001.5).

Existing `TestHandleAgentStalled_*` regressions in `event_handlers_stall_test.go`
keep passing unchanged.

## Files likely touched

- `apps/backend/internal/orchestrator/event_handlers_stall.go`
- `apps/backend/internal/orchestrator/event_handlers_stall_prolonged_test.go`

## Dependencies

Task 01. Without the lifecycle publishing the classification, the orchestrator
branch is not reachable.

## Risks

- Teardown must stay detached and bounded so a hung daemon cannot wedge the event
  bus (mirroring `neverStartedStopTimeout`).
- Do not route through `StopByTaskID`; it would write a cancellation state over the
  recorded terminal outcome.

## Parallelism

`sequential`

## Inputs

- `docs/specs/agents/system-design/agent-stall-inactivity-teardown.md`
- `apps/backend/internal/orchestrator/event_handlers_stall.go` (never-started
  precedent)

## Results

Done in fork `main` by PR #26, squash `d6c189026e`: `event_handlers_stall.go`
(+88/-…) generalizing the never-started seam to both classifications, plus the new
`event_handlers_stall_prolonged_test.go` (215 lines) citing AC-001.2 and AC-001.5.