---
created: 2026-10-04
status: completed
requirements:
  - REQ-AGENTS-SESSION-CEILING-003
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
legacy_specs: []
---

# Implementation Plan: Control-lane session capacity

## Overview

Give control and monitoring sessions their own bounded capacity so supervision
stays possible while the worker lane is saturated, without lowering the worker
ceiling an operator has already chosen.

The design compares three policies and adopts an additional bounded control
ceiling: the worker ceiling stays at its configured value and a separate control
ceiling admits control sessions in addition to it. An alternative that reserves a
lane inside the unchanged total was rejected because it permanently reduces
simultaneous worker capacity, and full control exemption was rejected because it
has no finite memory bound.

The
[requirements](../../specs/agents/requirements/session-concurrency-ceiling.md)
and [design](../../specs/agents/system-design/session-concurrency-ceiling.md)
extend the existing agent-owned admission contract. Session classification is
derived from durable agent-profile metadata and never from a caller-supplied
priority.

## Scope

### In scope

- Lane classification inside the single existing admission controller.
- A saved control ceiling and control profile set with their own environment
  override, resolved independently of the worker ceiling.
- Deferral, replay, restart reconstruction, and non-termination on cap decrease.
- The settings surface showing the worker ceiling, control ceiling, and total.
- Admission observability labelled by lane and reason only.

### Out of scope

- Per-workspace or per-user ceilings.
- Changing workflow WIP limits or limiting manual launches.
- A release toggle, YAML setting, or CPU-derived capacity selection.
- Replacing existing launch seams or the task repository.

## Work packages

- [Task 01: Control-lane admission and settings](task-01-control-lane-admission.md)

## Memory implication

The additional bounded control ceiling adds a worst-case incremental process
budget equal to the control ceiling times the per-process resident memory of one
concurrently running agent. The bound is linear and modeled, not measured:
representative per-process resident memory for worker and control agents must be
captured before an operator raises any ceiling above its current setting.