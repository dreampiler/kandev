---
id: "02-terminal-teardown"
title: "Record and tear down a terminally-stalled execution"
status: pending
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
the session leaves `RUNNING`/`STARTING`, then requests a forced, bounded
teardown. The ceiling slot returns through the existing reservation accounting.

## In scope

- In `handleAgentStalled` (`apps/backend/internal/orchestrator/event_handlers_stall.go`),
  add a branch for the terminal classification. Reuse the ownership guards
  (execution, prompt generation, activity epoch) already in place.
- Record the terminal outcome first, then request teardown through the existing
  forced, bounded stop seam (`stopNeverStartedExecution` generalized, or an
  equivalent `StopAgentWithReason` force call).
- Ensure the recorded settlement moves the session out of `RUNNING`/`STARTING`
  so the ceiling reservation is released.

## Out of scope

- The lifecycle classification (Task 01).
- Advancing the workflow step or applying a completion signal the agent did not
  request.
- Changing the never-started branch or its notice copy.

## Acceptance

1. A terminal classification records the terminal outcome and issues exactly one
   forced stop for the payload's execution.
2. A failed teardown keeps the recorded terminal state authoritative and leaves
   the execution registered.
3. The session leaves `RUNNING`/`STARTING`, so with the ceiling enabled its slot
   is returned and a deferred automatic launch becomes eligible.

## Verification

```sh
cd apps/backend && go test ./internal/orchestrator/... -count=1
```

New regressions in
`apps/backend/internal/orchestrator/event_handlers_stall_test.go`:

- `TestHandleAgentStalled_ProlongedStallRecordsAndStops` — a fake agent manager
  records stop calls; asserts one forced stop and a terminal session state. It
  must first fail with zero stop calls.
- `TestHandleAgentStalled_ProlongedStallKeepsTerminalStateWhenStopFails` — the
  fake returns an error; asserts the session stays terminal with the message.
- `TestHandleAgentStalled_ProlongedStallDoesNotAdvanceStep` — no step transition
  or completion signal is applied.

Existing `TestHandleAgentStalled_*` regressions must keep passing unchanged.

## Files likely touched

- `apps/backend/internal/orchestrator/event_handlers_stall.go`
- `apps/backend/internal/orchestrator/event_handlers_stall_test.go`

## Dependencies

Task 01. Without the lifecycle publishing the terminal classification, the
orchestrator branch is not reachable.

## Risks

- Teardown must stay detached and bounded so a hung daemon cannot wedge the
  event bus (mirror `neverStartedStopTimeout`).
- Do not route through `StopByTaskID`; it would write a cancellation state over
  the recorded terminal outcome.

## Parallelism

`sequential`

## Inputs

- `docs/specs/agents/system-design/agent-stall-inactivity-teardown.md`
- `apps/backend/internal/orchestrator/event_handlers_stall.go` (never-started
  precedent)

## Results

Pending.
