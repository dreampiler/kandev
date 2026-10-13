---
id: "01-workflow-entry"
title: "Preserve destination entry after an ended or unlaunched source"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WORKFLOW-PROFILE-SESSIONS-001
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
acceptance_criteria:
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-001.2
  - AC-TASKS-WORKFLOW-PROFILE-SESSIONS-001.14
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.1
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.3
system_design:
  - ../../specs/tasks/system-design/workflow-profile-session-lifecycle.md
  - ../../specs/agents/system-design/dynamic-agent-routing-01.md
---

# Workflow entry repair

## Scope and ownership

Own `event_handlers_workflow.go`, `task_operations.go`, `workflow_profile_session_lifecycle.go`,
`workflow_unlaunched_entry.go`, and the two corresponding regression files.
Update the existing private park-claim test call for its explicit terminal-source
policy argument.
Reuse the existing session guard and created-session launch. Leave ceiling
admission, deferred-launch claims, queue-promotion ownership, and task-stop
semantics in their existing owners.

## Acceptance

- A terminal predecessor is not reopened or parked again. A prepared
  destination remains primary when its predecessor already ended.
- Natural terminal-execution evidence on a nonterminal predecessor retains
  the existing park refusal, and metadata or queue-transfer failures retain
  rollback behavior.
- A waiting conversation without executor or native identity uses initial
  start only if it has no prompt history, or its recorded initial dynamic
  selection has never chosen an execution profile. Preserve the same session.
- A historical conversation without executor evidence is not cold-started.

## Verification and results

RED: `TestWorkflowReplacementSurvivesTerminalPredecessor` failed for CANCELLED,
COMPLETED, and FAILED sources with `cannot park workflow profile session`.
`TestWorkflowEntryStartsWaitingSessionWithoutExecutor` failed with
`session is not resumable: no executor record (state: WAITING_FOR_INPUT)`.

Focused command:

```text
go test -trimpath ./internal/orchestrator -run 'TestWorkflowEntry|TestWorkflowReplacementSurvivesTerminalPredecessor|TestParkSessionForProfileSwitch' -count=1
```

Run the changed orchestrator package, compare any Windows baseline failures
against unmodified main, and run the entry/lifecycle boundary regressions with
`-race`. Final results are recorded when these checks finish.

The non-race main comparison also reproduces
`TestAutoPromotionPreservesLogicalProfileAndNewestSession` in addition to the
two known passthrough attachment tests. A new terminal-admission regression
found by the full suite was repaired through the existing workflow-terminalized
error, preserving its queued handoff. A deterministic channel/synctest check
also preserves unguarded departure's synchronous source teardown.

Final GREEN (race):

```text
go test -trimpath -race -p 2 ./internal/orchestrator -run 'TestWorkflowEntry|TestWorkflowDeparture|TestWorkflowReplacementSurvivesTerminalPredecessor|TestWorkflowAutoStartRejectsSessionTerminalizedBeforeResume|TestParkSessionForProfileSwitch|TestAutoStartCreatedLaunch' -count=1
```

Passed. The full `go test -trimpath ./internal/orchestrator -count=1` comparison
has exactly the three failures also observed on unmodified main; there are no
new failures. The final admission-boundary correction preserves the existing
busy-launch error and merged-handoff queue path.
