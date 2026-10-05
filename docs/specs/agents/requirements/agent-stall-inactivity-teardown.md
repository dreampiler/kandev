---
status: active
system: agents
created: 2026-09-30
updated: 2026-10-05
owners:
  - Kandev
---

# Prolonged Stall Inactivity Teardown Requirements

## Overview

A prompt that has produced turn events and then goes permanently silent holds its
session in `RUNNING`/`STARTING` and consumes a session-ceiling slot until an
operator intervenes. The advisory stall notice offers recovery but does not
happen by itself. This capability adds a bounded automatic exception: past the
escalation threshold the prompt is classified terminal, the process is torn down,
and the session leaves `RUNNING`/`STARTING` so its ceiling slot is returned.

This belongs to the agent system because it extends the prompt watchdog's
classification and the terminal teardown the agent system already owns. The
threshold's tool-progress policy is owned by
[foreground tool stall progress](tool-stall-progress.md); this document owns only
the terminal consequence.

## Terminology

- **Escalation threshold:** `AgentExecution.stallThreshold()` — the ordinary
  fifteen-minute foreground inactivity allowance when no top-level tool is
  executing, and the bounded forty-five-minute allowance while one is.
- **Turn event:** The same definition as
  [agent stall recovery](agent-stall-recovery.md): assistant text, reasoning, a
  tool call or tool update, a plan update, or a permission request.

## Requirements

### REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001: Reclaim a prolonged silent prompt

**Intent:** A provider connection that drops without a timeout leaves a live
process emitting no turn events. Left in `RUNNING`/`STARTING`, enough of these
saturate the session ceiling and block all automatic launches. Past the escalation
threshold the system must reclaim the prompt and return its ceiling slot without
waiting for the operator.

#### Acceptance criteria

- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.1:** When a `RUNNING` or `STARTING`
  prompt produces no turn event and receives no user input past the escalation
  threshold, the system shall classify the prompt terminal and settle the session
  out of `RUNNING`/`STARTING`.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.2:** When the terminal classification
  is applied, the system shall stop the agent execution so no agent process for
  that session remains running, and shall record the terminal outcome even when
  the stop fails.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.3:** The escalation clock shall measure
  time since the last turn event or user input. A metadata frame shall not restart
  it, and a turn event or user input within the window shall prevent the terminal
  classification.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.4:** A prompt still inside the
  escalation window shall keep the existing five-minute advisory notice and Cancel
  turn action, with no state change, no teardown, and no ceiling-slot change.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.5:** The terminal settlement shall not
  advance the workflow step or apply a `step_complete_kandev` signal the agent did
  not request. It only reclaims the prompt and its ceiling slot.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.6:** When the ceiling is enabled, a
  reclaimed session shall return its slot so a deferred automatic launch becomes
  eligible on the next retry sweep.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.7:** A never-started prompt shall not be
  reclassified as prolonged; the two terminal classifications are mutually
  exclusive and the never-started path is unchanged.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.8:** A prompt whose escalation
  threshold is the forty-five-minute foreground-tool allowance shall keep that
  allowance, and observed tool progress shall continue to refresh it.

## Out of scope

- Automatically timing out or cancelling a prompt that is still emitting turn
  events, however slow.
- The threshold's tool-progress policy and its configurable-looking durations;
  those belong to `REQ-AGENTS-TOOL-STALL-PROGRESS-001`.
- Bounding the unbounded ACP `Prompt` call with a deadline; that is a separate
  adapter-level decision.
- Changing the workflow step for the reclaimed task, or synthesizing a
  `step_complete_kandev` signal. Releasing the blocked prompt wait with a
  synthetic completion is in scope and is not a workflow signal.
- Office-specific quorum or step-advance behavior; the stuck-signal watchdog's
  Office exclusion is not lifted here.