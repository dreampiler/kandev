---
id: "01-terminal-classification"
title: "Classify a silent prompt as terminal past the escalation threshold"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001
acceptance_criteria:
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.1
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.3
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.4
  - AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.7
system_design:
  - ../../specs/agents/system-design/agent-stall-inactivity-teardown.md
---

# Task 01: Classify a silent prompt as terminal past the escalation threshold

## Summary

`waitForPromptDone` gains a terminal branch at the execution's escalation
threshold. Past it, the watchdog publishes a terminal stall classification once per
prompt generation and injects a synthetic completion so the in-flight wait returns.
The five-minute advisory branch is unchanged, and a never-started prompt is never
reclassified.

## In scope

- In `waitForPromptDone`
  (`apps/backend/internal/agent/runtime/lifecycle/session.go`), after the existing
  `>= 5m` advisory branch, add a branch at `elapsed >= execution.stallThreshold()`
  that publishes the terminal classification and injects the synthetic completion.
- Carry the classification on `AgentStalledPayload` (`event_types.go`) as
  `ProlongedStall`, leaving the advisory fields intact and keeping it mutually
  exclusive with `NeverStarted`.
- Use the existing `promptActivitySnapshot()` clock and `stallThreshold()`; do not
  add a second clock or a second threshold.
- Publish at most once per prompt generation, with the latch keyed to the activity
  epoch so a resumed prompt can still be classified in a later epoch.

## Out of scope

- The orchestrator's handling of the classification (Task 02).
- Any change to the five-minute advisory notice, copy, or the never-started path.
- The tool-progress policy that supplies the longer executing-tool allowance.
- Bounding the ACP `Prompt` call.

## Acceptance

1. A prompt with a turn event then silence past the threshold publishes the terminal
   classification exactly once for that generation.
2. A turn event or user input inside the window bumps the activity epoch and
   prevents the classification.
3. The five-minute advisory notice and its Cancel turn action remain unchanged and
   still do not change state or tear down.
4. A never-started prompt is not reclassified as prolonged.

## Verification

```sh
cd apps/backend && go test ./internal/agent/runtime/lifecycle/... -count=1
```

Regressions live in
`apps/backend/internal/agent/runtime/lifecycle/stall_terminal_test.go` and
`stall_escalation_test.go`:

- The terminal classification publishes once for a silent prompt past the
  threshold.
- A turn event inside the window prevents the classification.
- The advisory branch still fires and does not publish the terminal
  classification.
- A never-started prompt is not reclassified.

Existing stall regressions in `session_test.go` and `stall_activity_test.go` keep
passing unchanged.

## Files likely touched

- `apps/backend/internal/agent/runtime/lifecycle/session.go`
- `apps/backend/internal/agent/runtime/lifecycle/event_types.go`
- `apps/backend/internal/agent/runtime/lifecycle/events.go`
- `apps/backend/internal/agent/runtime/lifecycle/stall_terminal_test.go`

## Dependencies

None.

## Risks

- The synthetic completion must not race a real completion; reuse the existing
  startup/prompt-generation guards.
- Production durations must not be shortened to make a test pass; use
  `testing/synctest` or a test-scoped threshold like the existing stall tests.

## Parallelism

`sequential`

## Inputs

- `docs/specs/agents/system-design/agent-stall-inactivity-teardown.md`
- `docs/specs/agents/requirements/agent-stall-recovery.md` (shared clock)

## Results

Done in fork `main` by PR #26, squash `d6c189026e`: `event_types.go` (+6),
`events.go` (+9/-…), `session.go` (+35/-…), and the new
`stall_terminal_test.go` (261 lines) covering AC-001.1, AC-001.3, and AC-001.4.

AC-001.8, the executing-tool allowance, is implemented in `tool_progress.go`
rather than covered here: `stallThreshold()` selects the longer allowance for
an executing tool, and `stall_escalation_test.go` exercises the threshold
branch without any open tool. No test populates `openTools`, so the
forty-five-minute branch itself remains unexercised.