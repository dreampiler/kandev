---
id: "01-orphaned-route-relaunch"
title: "Relaunch an orphaned route without an executor"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.27
system_design:
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
---
# Task 01: Relaunch an orphaned route without an executor

## Summary

Let Retry and Try next launch a session whose orphaned route has no executor
row, and deliver its held queued message once.

## In scope

- `dynamic_launch.go`: the absent-predecessor classification in
  `stopDynamicRelaunchPredecessor` and the resume-source check that picks the
  manual launch mode.
- `dynamic_launch_orphan_route.go`: the two relaunch helpers that the file
  length limit keeps out of `dynamic_launch.go` (resume-source lookup and the
  queue-owned message skip).
- `event_handlers_agent.go`: share the queued transcript message ID helper.

## Out of scope

The queue hold, the orphan sweep, and the route-action handler.

## Acceptance

- AC 001.27: Retry and Try next launch one fresh execution, the route leaves
  `action_required`, and the held message is delivered once. An empty
  predecessor ID counts as stopped, and a genuine stop error still fails.

## Verification

```bash
cd apps/backend && go test ./internal/orchestrator/ -run 'TestLaunchDynamicRouteAction_|TestStopDynamicRelaunchPredecessor_|TestRelaunchDynamicTaskAfterFailure_|TestLaunchPreparedDynamicRelaunch|TestReconcileOrphanedDynamic'
```

## Results

Done. RED was observed on the base revision (all four relaunch subtests failed
with `dynamic route action successor launch failed`; the empty predecessor ID
case returned `false`). GREEN was observed on the branch head: the four relaunch
subtests, the three predecessor-classification subtests, and the neighbouring
route-action, relaunch, prepared-relaunch, and orphan-sweep tests passed. The
exact commands and outputs are recorded in
[the plan](plan.md#verification-results).
