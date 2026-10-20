---
status: current
system: tasks
requirements:
  - REQ-TASKS-STALE-CREATED-AUTOSTART-001
created: 2026-10-20
owners:
  - kandev
---

# Stale CREATED auto-start system design

## Purpose and boundaries

This design classifies a lost CREATED race at the STARTING persistence boundary
and lets the workflow auto-start dispatch continue from the state that won. It
does not change how other paths move sessions, how prompts are queued, or how
genuine launch failures are recorded.

## Components

| Surface | Responsibility |
| --- | --- |
| `executor.ErrSessionStateChangedBeforeStart` | Marks a refused STARTING write whose stored session is non-terminal and owns no agent |
| `Service.persistFullTaskSessionIfCurrent` | Orchestrator STARTING persistence; wraps the marker for CREATED and WAITING_FOR_INPUT |
| `Executor.persistSessionFullRowIfCurrentState` | Executor fallback persistence; wraps the same marker |
| `Service.startCreatedSession` | Returns the marked error without `handleSessionLaunchFailure`, completing the turn it opened |
| `Service.launchCreatedAutoStartStepPrompt` | Routes a stale snapshot or a lost race to `continueAutoStartAfterCreatedStateLost` |
| `Service.continueAutoStartAfterCreatedStateLost` | Delivers through `retryAutoStartStepPrompt` or returns the hand-off to the queue |

## Persistence classification

The guarded STARTING write already classifies its refusals. A terminal session
yields `SessionStateSupersededError`; STARTING and RUNNING yield
`ErrExecutionAlreadyRunning`. The remaining non-terminal states, CREATED and
WAITING_FOR_INPUT, own no agent, so the refusal means another path advanced the
session and this launch has nothing to clean up beyond the execution it already
stops. Those refusals wrap `ErrSessionStateChangedBeforeStart`. The message text
is unchanged apart from the appended marker.

`startCreatedSession` treats the marker like `ErrExecutionAlreadyRunning`: it
completes the turn it opened and returns the error. It does not call
`handleSessionLaunchFailure`, which is the call that wrote FAILED to the session
and, through `updateTaskStateForEarlyLaunchFailure`, to the task.

## Auto-start continuation

`launchCreatedAutoStartStepPrompt` reloads the session under the
cancel-in-flight guard. A terminal session keeps its terminalized error. A
session that is no longer CREATED releases the guard and continues without
launching. A launch that returns the marker retires its start attempt, releases
the guard, reloads the session, and continues the same way.

The continuation reads the task. When the session is WAITING_FOR_INPUT and the
task is still on the dispatching step, it delivers the already recorded step
prompt through `retryAutoStartStepPrompt`, the path a non-CREATED snapshot takes.
The losing launch failed before starting the agent process or registering the
initial prompt dispatch, so this is the only delivery. Otherwise it returns the
taken hand-off message to the queue and returns nil.

## Verification

`event_handlers_stale_created_autostart_test.go` runs the production shape with
a prepared workspace and in-process fakes: the session advances to
WAITING_FOR_INPUT between the launch's reload and its STARTING write, with and
without a step move, and a replay whose snapshot is already stale.
