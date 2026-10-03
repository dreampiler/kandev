---
id: "01-terminal-classification"
title: "Classify a 15-minute silent prompt as terminal"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001
acceptance_criteria:
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.1
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.3
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.4
system_design:
  - ../../specs/agents/system-design/agent-stall-inactivity-teardown.md
---

# Task 01: Classify a 15-minute silent prompt as terminal

## Summary

`waitForPromptDone` gains a second threshold. At 15 minutes of honest
inactivity, it publishes a terminal stall classification once per prompt
generation and injects a synthetic completion signal so the in-flight waiter
returns. The five-minute advisory branch is unchanged.

## In scope

- In `waitForPromptDone` (`apps/backend/internal/agent/runtime/lifecycle/session.go`),
  after the existing `>= 5m` advisory branch, add a `>= 15m` branch that
  publishes the terminal classification via `EventPublisher` and injects the
  synthetic completion signal.
- Carry the terminal classification on `AgentStalledPayload`
  (`event_types.go`) with a new discriminator field, leaving the advisory fields
  intact.
- Use the existing `promptActivitySnapshot()` clock; do not add a second clock.
- Publish the terminal classification at most once per prompt generation.

## Out of scope

- The orchestrator's handling of the terminal classification (Task 02).
- Any change to the five-minute advisory notice, copy, or the never-started path.
- Bounding the ACP `Prompt` call.

## Acceptance

1. A prompt with a turn event then 15 minutes of silence publishes the terminal
   classification exactly once for that generation.
2. A turn event or user input inside the window bumps the activity epoch and
   prevents the terminal classification.
3. The five-minute advisory notice and its Cancel turn action remain unchanged
   and still do not change state or tear down.

## Verification

```sh
cd apps/backend && go test ./internal/agent/runtime/lifecycle/... -count=1
```

New regressions in
`apps/backend/internal/agent/runtime/lifecycle/stall_activity_test.go` (or a
new sibling file if that file is near the revive line limit):

- A test using the real one-minute ticker and a shortened threshold that
  publishes the terminal classification once for a silent prompt.
- A test that a turn event inside the window prevents the classification.
- A test that the advisory branch still fires at the five-minute mark and does
  not publish the terminal classification.

Existing stall regressions in `session_test.go` and `stall_activity_test.go`
must keep passing unchanged.

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/session.go`
- `apps/backend/internal/agent/runtime/lifecycle/event_types.go`
- `apps/backend/internal/agent/runtime/lifecycle/events.go`
- `apps/backend/internal/agent/runtime/lifecycle/stall_activity_test.go`

## Dependencies

None.

## Risks

- The synthetic completion signal must not race a real completion; reuse the
  existing startup/prompt-generation guards.
- Production durations must not be shortened to make a test pass; use
  `testing/synctest` or a test-scoped threshold like the existing stall tests.

## Parallelism

`sequential`

## Inputs

- `docs/specs/agents/system-design/agent-stall-inactivity-teardown.md`
- `docs/specs/agents/requirements/agent-stall-recovery.md` (shared clock)

## Results

Pending.
