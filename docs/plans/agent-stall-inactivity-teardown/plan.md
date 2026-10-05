---
created: 2026-09-30
status: implemented
requirements:
  - REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001
system_design:
  - ../../specs/agents/system-design/agent-stall-inactivity-teardown.md
legacy_specs: []
---

# Implementation Plan: Prolonged Stall Inactivity Teardown

## Overview

Extend the prompt watchdog so a `RUNNING`/`STARTING` prompt that has produced turn
events but then goes silent past the escalation threshold is classified terminal and
torn down, returning its session-ceiling slot. The change is a bounded exception to
user-controlled recovery, not a replacement for the five-minute advisory notice.

The order is: first the lifecycle watchdog gains the terminal branch and the
synthetic completion (Task 01), then the orchestrator records the terminal outcome
and requests forced teardown (Task 02). Task 02 depends on Task 01 because the
orchestrator handler only sees the terminal classification after the lifecycle
publishes it.

## Scope

### In scope

- A terminal classification in the prompt watchdog at the execution's escalation
  threshold, using the existing honest inactivity clock.
- A synthetic completion that releases the in-flight prompt wait.
- Orchestrator record-then-teardown for the terminal classification, reusing the
  shared teardown seam.
- Regressions proving a prolonged silent prompt is reclaimed and its ceiling slot
  returns, while the five-minute advisory behavior is unchanged.

### Out of scope

- Bounding the unbounded ACP `Prompt` call with a deadline.
- The threshold's tool-progress policy; that belongs to
  `REQ-AGENTS-TOOL-STALL-PROGRESS-001`.
- Changing the workflow step, or synthesizing a `step_complete_kandev` signal.
  Releasing the blocked prompt wait with a synthetic completion is in scope.
- Office-specific quorum or step-advance behavior.

## Technical approach

The honest clock, the stall payload, the stall threshold selector, and the
record-then-teardown seam all already exist. This plan reuses all of them and adds
only the terminal branch and its classification.

- `apps/backend/internal/agent/runtime/lifecycle/session.go` — `waitForPromptDone`:
  add the terminal branch after the existing five-minute advisory branch; publish
  the terminal stall once per generation and inject the synthetic completion so the
  wait returns.
- `apps/backend/internal/agent/runtime/lifecycle/event_types.go` / `events.go` —
  carry the classification on `AgentStalledPayload` as `ProlongedStall`, without
  disturbing the advisory fields.
- `apps/backend/internal/orchestrator/event_handlers_stall.go` — handle the
  classification with the same ownership guards, record the terminal outcome, and
  request teardown through the existing `recordAndStopStalledExecution` /
  `stopStalledExecution` seam.

No schema migration, no HTTP or WebSocket contract change, no frontend change.

## Tests

| Acceptance criterion | Evidence |
| --- | --- |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.1` | `stall_terminal_test.go`: a silent prompt past the threshold publishes the terminal classification exactly once |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.2` | `event_handlers_stall_prolonged_test.go`: the classification records the outcome and issues one forced stop |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.3` | `stall_terminal_test.go`: a turn event inside the window bumps the epoch and prevents the classification |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.4` | `stall_terminal_test.go`: the advisory branch still fires and does not publish the terminal classification |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.5` | `event_handlers_stall_prolonged_test.go`: no step transition or completion signal is applied |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.7` | `stall_terminal_test.go`: a never-started prompt is not reclassified |
| `AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.8` | Implementation only, no direct test: `stallThreshold()` (`tool_progress.go:10-19`) returns the longer allowance for an executing tool. No repository test populates `openTools`, so that branch is not directly exercised |

## Work orders

- [x] [Task 01: Classify a silent prompt as terminal past the escalation threshold](task-01-terminal-classification.md)
- [x] [Task 02: Record and tear down a terminally-stalled execution](task-02-terminal-teardown.md)

## Verification results

Implemented in fork `main` by PR #26, squash `d6c189026e`. That commit is an
ancestor of the current `main` head and changed six files: the two lifecycle event
files, `session.go`, `event_handlers_stall.go`, and the two new regression files
`stall_terminal_test.go` and `event_handlers_stall_prolonged_test.go`. The
lifecycle tests cite AC-001.1, AC-001.3, and AC-001.4; the orchestrator tests cite
AC-001.2 and AC-001.5. `AC-001.6` is satisfied by the unchanged ceiling
reservation accounting that releases on settlement, and AC-001.7 by the
never-started exclusion the lifecycle applies before publishing.

`AC-001.8` is satisfied by implementation rather than by a test.
`AgentExecution.stallThreshold()` (`tool_progress.go:10-19`) returns the
forty-five-minute allowance while an open top-level tool is executing and the
fifteen-minute one otherwise; `applyToolProgress` (`tool_progress.go:71-110`)
updates `lastToolProgressAt` and the activity epoch on observed CPU or status
change, and `promptStallSnapshot` (`tool_progress.go:112-122`) admits
`lastToolProgressAt` when measuring inactivity. No repository test populates
`openTools`, so the longer allowance is not directly exercised. Closing that gap
needs a standing test, which is an owner decision rather than part of this plan.

## Risks

- The terminal threshold tears down a legitimate long-running tool once its
  allowance expires. Accepted in the ADR; an executing tool keeps the longer
  forty-five-minute allowance and validated progress refreshes it.
- The synthetic completion must not race a real completion. The existing
  startup/prompt-generation guards reject a stale signal.
- The teardown must stay detached and bounded so a hung daemon cannot wedge the
  event bus.