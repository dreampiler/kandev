---
id: "01-record-stream-stages"
title: "Record the first missing update-stream stage"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-UPDATE-STREAM-CONTINUITY-001
acceptance_criteria:
  - AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.4
  - AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.5
system_design:
  - ../../specs/agents/system-design/update-stream-continuity.md
---

# Task 01: Record the First Missing Update-Stream Stage

## Summary

Add bounded stage counters and one first-stall record that separates agentctl
forwarding, WebSocket receipt, and backend handler progress without logging
message contents. This is the evidence requested when the historical stall's
first blocked stage cannot be recovered.

## In scope

- First trace the two incomplete stop paths through `Executor.StopExecution`,
  `Manager.StopAgentWithReason`, lifecycle activity acquisition, created-
  execution publication, and synchronous event subscribers. Record which
  operations hold `remoteInstanceLifecycleMu` while publishing and which
  subscriber paths can wait for that lock. Use the 08:00:44 and 11:54:03
  execution IDs in the task plan to bound this investigation; do not infer a
  deadlock from the missing `stopping agent` log alone.
- If existing receipts cannot distinguish mutex wait from activity or event-
  subscriber wait, add a bounded first-stall lock/phase diagnostic, including
  a goroutine snapshot to a local diagnostic file when available. Capture
  once per incident without raw prompts or credentials. Make collection
  independent of the blocked synchronous event publication path.
- Track the last adapter update enqueue in
  `internal/agentctl/server/process`, the last stream send in
  `internal/agentctl/server/api/agent.go`, and the last read/handler completion
  in `internal/agent/runtime/agentctl/agent.go`.
- Convey only stage counters on a bounded internal transport heartbeat. Keep
  it out of the agent event and prompt activity clocks.
- Have the lifecycle watchdog snapshot and emit one structured line before
  `agent.stalled` publication, with unavailable fields where the upstream
  stage has no recent receipt.

## Exclusions

- No prompt body, raw frame, credential, unbounded identifier metric label,
  new public endpoint, UI copy, or premature root-cause assertion.

## Implementation acceptance

1. A blocked ordered handler with a continuing stream read produces different
   stage ages than an adapter/agentctl silence or a stale WebSocket.
2. One first-stall log per prompt generation reports bounded ages/availability
   even when the synchronous stall subscriber fails to return.
3. Heartbeats and metadata never advance the honest turn activity clock.

## Verification

From `apps/backend`, run only the changed packages and no listener tests:

```text
go test -p 2 -tags fts5 -run '^TestUpdateStreamStage' ./internal/agentctl/server/process ./internal/agentctl/server/api ./internal/agent/runtime/agentctl ./internal/agent/runtime/lifecycle
```

## Dependencies and risks

No prerequisite. Keep stage state per exact execution/stream generation and
make snapshots safe while the event worker is blocked. Test the classifier
with channels/fakes rather than an HTTP or WebSocket listener.

## Results

Pending.
