---
status: draft
system: agents
created: 2026-09-30
owners:
  - Kandev
---

# Prolonged Stall Inactivity Teardown Requirements

## Overview

A prompt that has produced turn events and then goes permanently silent holds its
session in `RUNNING`/`STARTING` and consumes a session-ceiling slot until an
operator intervenes. The advisory stall notice offers recovery but does not
happen by itself. This capability adds a bounded automatic exception: after a
prolonged silence, the prompt is classified terminal, the process is torn down,
and the session leaves `RUNNING`/`STARTING` so its ceiling slot is returned.

This belongs to the agent system because it extends the prompt watchdog's
classification and the terminal teardown the agent system already owns.

## Terminology

- **Prolonged silence:** A `RUNNING`/`STARTING` prompt whose honest inactivity
  clock shows no turn event and no user input for 15 minutes.
- **Turn event:** The same definition as
  [agent stall recovery](agent-stall-recovery.md): assistant text, reasoning,
  a tool call or tool update, a plan update, or a permission request.

## Requirements

### REQ-AGENTS-PROLONGED-STALL-TEARDOWN-001: Reclaim a prolonged silent prompt

**Intent:** A provider connection that drops without a timeout leaves a live
process emitting no turn events. Left in `RUNNING`/`STARTING`, enough of these
saturate the session ceiling and block all automatic launches. After a prolonged
silence the system must reclaim the prompt and return its ceiling slot without
waiting for the operator.

#### Acceptance criteria

- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.1:** When a `RUNNING` or `STARTING`
  prompt produces no turn event and receives no user input for 15 minutes, the
  system shall classify the prompt terminal and settle the session out of
  `RUNNING`/`STARTING`.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.2:** When the terminal classification
  is applied, the system shall stop the agent execution so no agent process for
  that session remains running, and shall record the terminal outcome even when
  the stop fails.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.3:** The 15-minute clock shall measure
  time since the last turn event or user input. A metadata frame shall not
  restart it, and a turn event or user input within the window shall prevent the
  terminal classification.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.4:** A prompt still inside the
  15-minute window shall keep the existing five-minute advisory notice and
  Cancel turn action, with no state change, no teardown, and no ceiling-slot
  change.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.5:** The terminal settlement shall not
  advance the workflow step or apply a completion signal the agent did not
  request. It only reclaims the prompt and its ceiling slot.
- **AC-AGENTS-PROLONGED-STALL-TEARDOWN-001.6:** When the ceiling is enabled, a
  reclaimed session shall return its slot so a deferred automatic launch becomes
  eligible on the next retry sweep.

## Out of scope

- Automatically timing out or cancelling a prompt that is still emitting turn
  events, however slow.
- A configurable threshold. The 15-minute bound is fixed for this version.
- Bounding the unbounded ACP `Prompt` call with a deadline; that is a separate
  adapter-level decision.
- Changing the workflow step for the reclaimed task, or synthesizing a
  `step_complete_kandev` signal.
- Office-specific quorum or step-advance behavior; the stuck-signal watchdog's
  Office exclusion is not lifted here.
