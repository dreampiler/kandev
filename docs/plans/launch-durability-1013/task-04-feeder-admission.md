---
id: "04-feeder-admission"
title: "Admit a queued feeder arrival before destination entry"
status: done
wave: 3
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-WIP-LIMIT-PULL-SYSTEM-001
acceptance_criteria:
  - AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.2
  - AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.4
  - AC-TASKS-WIP-LIMIT-PULL-SYSTEM-001.6
system_design:
  - ../../specs/tasks/system-design/wip-limit-pull-system.md
---

# Feeder admission repair

Own `workflow_store.go` and `workflow_feeder_admission_test.go`. Reuse the
existing atomic promotion, WIP fields, move event, queued-exit marker, and
deferred launch. No repository, schema, scheduler, or retry mechanism change.

The feeder path omitted the admission assignments that already exist in its
same-step sibling and in task service: `WIPAdmitted=true`, empty
`QueuedForStepID`, and nil `QueuedAt`. The repository persists the caller's
fields, so the promoted task could still be queued when entry evaluated it.

Production readback: `8b5c4925` has a `wip_pull` ledger row at 02:03Z, followed
by manual moves at 02:14Z and MCP moves at 02:15Z. Its 02:20Z launch was after
those workarounds. The earlier successful-launch observation did not explain
the original missing entry.

RED: `TestWorkflowFeederPromotionAdmitsQueuedTaskBeforeStarting` failed with
`feeder arrival must leave the queued band before entry claims its launch`.
The SQLite-backed test uses the real workflow store, move publisher, event
watcher, and deferred launcher, with only the agent boundary mocked. GREEN
passed after assigning the three existing admission fields.

```text
go test -trimpath ./internal/orchestrator -run TestWorkflowFeederPromotionAdmitsQueuedTaskBeforeStarting -count=1
```

Complete affected workflow-store/queue and previous repair regressions under
race, and compare the changed package against the already-recorded Windows
baseline. Record final results before delivery.

Final verification:

```text
go test -trimpath -race -p 2 ./internal/orchestrator -run 'TestWorkflowFeeder|TestWorkflowStore|TestHandleTaskQueue|TestQueuedMove|TestWorkflowEntry|TestWorkflowDeparture|TestWorkflowReplacementSurvivesTerminalPredecessor|TestParkSessionForProfileSwitch|TestAutoStartCreatedLaunch|TestWorkflowAutoStartRejectsSessionTerminalizedBeforeResume' -count=1
go test -trimpath ./internal/orchestrator -count=1
```

Focused race checks passed. The complete package failed only the same two
passthrough attachment tests and primary-promotion test recorded on the
unmodified baseline; no new failure remains. Documentation catalog passed.
