---
created: 2026-09-30
status: draft
requirements:
  - REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001
system_design:
  - ../../specs/agents/system-design/agent-stall-inactivity-teardown.md
legacy_specs: []
---

# Implementation Plan: Prolonged Stall Inactivity Teardown

## Overview

Extend the prompt watchdog so a `RUNNING`/`STARTING` prompt that has produced
turn events but then goes silent for 15 minutes is classified terminal and torn
down, returning its session-ceiling slot. The change is a bounded exception to
user-controlled recovery, not a replacement for the five-minute advisory notice.

The order is: first the lifecycle watchdog gains the 15-minute terminal branch
and the synthetic completion signal (Task 01), then the orchestrator records the
terminal outcome and requests forced teardown (Task 02). Task 02 depends on Task
01 because the orchestrator handler only sees the terminal classification after
the lifecycle publishes it.

## Scope

### In scope

- A 15-minute terminal classification in the prompt watchdog, using the existing
  honest inactivity clock.
- A synthetic completion signal that unblocks the in-flight `waitForPromptDone`
  waiter.
- Orchestrator record-then-teardown for the terminal classification, reusing the
  never-started teardown seam.
- Regression tests proving a prolonged silent prompt is reclaimed and its ceiling
  slot returns, while the five-minute advisory behavior is unchanged.

### Out of scope

- Bounding the unbounded ACP `Prompt` call with a deadline.
- A configurable threshold.
- Changing the workflow step, or synthesizing a completion signal.
- Office-specific quorum or step-advance behavior.

## Technical approach

The honest clock, the stall payload, and the record-then-teardown seam already
exist. This plan reuses all three and adds only the terminal branch and its
classification.

- `apps/backend/internal/agent/runtime/lifecycle/session.go` —
  `waitForPromptDone`: add the `>= 15m` branch after the existing five-minute
  advisory branch; publish the terminal stall once per generation and inject a
  synthetic completion signal so the waiter returns.
- `apps/backend/internal/agent/runtime/lifecycle/event_types.go` /
  `events.go` — carry the terminal classification on `AgentStalledPayload` (a
  new discriminator field) without disturbing the advisory fields.
- `apps/backend/internal/orchestrator/event_handlers_stall.go` — handle the
  terminal classification with the same ownership guards, record the terminal
  outcome, and request teardown through the existing
  `stopNeverStartedExecution`-style forced, bounded stop.

No schema migration, no HTTP or WebSocket contract change, no frontend change.

## Tests

| Acceptance criterion | Evidence |
| --- | --- |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.1` | lifecycle test: a 15-minute silent prompt publishes the terminal classification exactly once |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.2` | orchestrator test: terminal classification records the outcome and issues one forced stop |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.3` | lifecycle test: a turn event inside the window bumps the epoch and prevents the classification |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.4` | lifecycle/orchestrator test: the five-minute advisory branch and notice are unchanged |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.6` | ceiling test: a reclaimed session returns its reservation |

## Work orders

- [ ] [Task 01: Classify a 15-minute silent prompt as terminal](task-01-terminal-classification.md)
- [ ] [Task 02: Record and tear down a terminally-stalled execution](task-02-terminal-teardown.md)

## Verification results

Pending.

## Risks

- The 15-minute bound tears down a legitimate long-running tool that emits no
  turn event for 15 minutes. Accepted in the ADR; the bound is 3x the advisory
  threshold.
- The synthetic completion signal must not race a real completion. The existing
  startup/prompt-generation guards are reused to reject a stale signal.
- The teardown must stay detached and bounded so a hung daemon cannot wedge the
  event bus.
