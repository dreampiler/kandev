---
status: draft
system: tasks
requirements:
  - REQ-TASKS-DEFERRED-START-STEP-ENTRY-001
---

# Deferred Start on Step Entry System Design

## Purpose and boundaries

The orchestrator's automatic start path consumes a task's `deferred_launch`
record. This design limits when a `task.moved` entry may consume it.
[Workflow step agent start ownership](workflow-step-agent-start-ownership.md)
owns step prompts and start races. The session ceiling and WIP queue own their
own deferred records' replay.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-TASKS-DEFERRED-START-STEP-ENTRY-001` | [Control flow](#control-flow) |

## Components and responsibilities

- `autoStartTaskForStep` in `orchestrator/event_handlers_workflow.go` handles
  automatic starts for `task.moved` and the watcher.
- `autoStartTaskForLoadedStep` settles auto-start-on-create tokens, consumes the
  deferred record through `launchDeferredTask`, and checks the step's
  `auto_start_agent` action.

## Data and contracts

`autoStartTaskForLoadedStep` gains a `retainDeferredLaunch` argument. When it is
`true`, the function does not call `launchDeferredTask`. Every existing caller
passes `false`.

## Control flow

`autoStartTaskForStep` called `launchDeferredTask` before reading the
destination step, and `autoStartTaskForLoadedStep` called it again before its
own `auto_start_agent` check. A move into a hold step therefore launched the
record and removed it.

`autoStartTaskForStep` now reads the step first:

- For `task.moved` into a step without `auto_start_agent`, it calls
  `autoStartTaskForLoadedStep` with `retainDeferredLaunch=true`. Tokens are
  settled and the skipped start is logged, and the record stays (`001.1`).
- Otherwise it calls `launchDeferredTask` and then `autoStartTaskForLoadedStep`
  as before, so the next agent step consumes the record (`001.2`).
- When the step read fails, it still calls `launchDeferredTask` before it
  returns, because a failed read is no evidence that the step has no agent
  (`001.3`).

Queue promotion, dependency resolution, and creation call
`autoStartTaskForLoadedStep` with `false` and keep their behavior (`001.3`).

## Failure and recovery

A kept record is consumed by the next eligible entry or by an explicit start.
No retry or sweep is added.

## Persistence

No schema change. The record is left untouched in task metadata.

## Security

No permission or trust boundary changes.

## Observability

A debug log names the task and step whose deferred start was kept.
