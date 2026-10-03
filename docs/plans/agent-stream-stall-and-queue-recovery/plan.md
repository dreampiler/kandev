---
created: 2026-10-01
status: draft
requirements:
  - REQ-AGENTS-UPDATE-STREAM-CONTINUITY-001
  - REQ-TASKS-QUEUED-POST-DISPATCH-RECOVERY-001
system_design:
  - ../../specs/agents/system-design/update-stream-continuity.md
  - ../../specs/tasks/system-design/queued-post-dispatch-recovery.md
legacy_specs: []
---

# Implementation Plan: Agent Stream Stall and Queue Recovery

## Overview

Two whole-install incidents (08:01 and 11:54 KST, 2026-10-01) left several
Codex ACP prompts producing no backend turn events until restart or synthetic
completion. The later synthetic completions also left accepted queued
dispatches without a successor drain. Reconnect active prompts after a true
stream drop, record one stage-discriminating diagnostic at the first stall to
identify a silent-but-open path next time, and repair accepted-error queue
settlement. This package takes priority over peer-report batch delivery.

## Evidence and root cause status

- New operator evidence (2026-10-01 14:3x KST) narrows the common trigger:
  both incidents followed a stop request that did not log lifecycle stop
  completion. At 11:54:03, execution `a3d93fdf` entered `StopExecution` for
  parent MCP stop; at 11:54:30 its session received `mcp.step_complete` without
  a recorded result. At 08:00:44, execution `6c4a3b6c` entered cascade-archive
  stop after the preceding `026f9fe1` stop completed. The 11:48 disconnect
  belongs to an intentional parent stop, not the onset of the 11:54 incident.
  Missing completion logs narrow the interval but do not prove the wait site.
- Current source has `StopAgentWithReason` acquire
  `remoteInstanceLifecycleMu` and then a stopping activity lease before its
  `stopping agent` log. `registerAndPublishCreatedExecution` holds that same
  execution mutex through `publishCreatedExecution`. A lock/event-subscriber
  cycle is a plausible hypothesis, not an established root cause. The next
  investigation must distinguish mutex wait, activity acquisition, event
  publication, and subscriber wait before prescribing a lock-order repair.
- Confirmed: the MPM Codex ACP session continued to generate local Codex log
  records after the backend's last turn activity. The affected executions
  used distinct agentctl ports; backend MCP requests and persistence-health
  logs continued. Several early closed-network warnings coincide with
  explicit stop operations and are not the cause receipt.
- Confirmed code gap: `StreamManager` reconnects after backend restart, and
  `dispatchPrompt` retries an absent stream for a new prompt, but an
  unexpected mid-turn drop fails the active execution without bounded
  reattachment. No frame-stage receipt proves that this gap caused 11:54.
- Confirmed queue defect: accepted post-dispatch errors acknowledge the
  accepted queue row while the caller's next-drain condition is `err == nil`.
  The 12:09-12:12 logs show three instances of that path. Remaining rows
  waited for restart.
- Unresolved first blocked stage: retained logs do not separate adapter
  forwarding, agentctl writer, backend WebSocket receipt, and the ordered
  handler worker, nor the stop-path wait site. Task 01 first traces the
  stop/publication lock graph and supplies bounded next-occurrence evidence
  for the unresolved stage rather than attributing the stall to an unsupported
  cause. Do not treat stream reattachment as the repair for an open but blocked
  event path.

## Scope

### In scope

- Internal stage liveness metadata and one bounded first-stall log line.
- Bounded same-execution reattachment after a genuinely unexpected stream
  close, with retained outcome reconciliation and no prompt replay.
- Successor queue evaluation after an accepted prompt's post-dispatch error,
  respecting session recovery and all existing delivery gates.

### Out of scope

- Reimplementing the existing fifteen-minute prolonged-stall teardown or
  changing its user-visible classification.
- Raising the queue cap, peer-report batching, a whole-install restart,
  frontend controls, or operating installation replacement.
- Listener-opening tests, the full `go test ./...` suite, and E2E.

## Technical approach

The stage record is grounded at `agentctl/server/process` update enqueue,
`agentctl/server/api` WebSocket send, `agent/runtime/agentctl` WebSocket read,
and its ordered worker completion. An internal heartbeat conveys agentctl
stage counters without resetting prompt activity. The lifecycle watchdog
logs the snapshot before synchronous `agent.stalled` publication.

The stream manager distinguishes intentional teardown from an unexpected
close. It reconnects to the same agentctl instance under execution/startup/
prompt-generation guards, does not re-send the prompt, and reconciles any
retained terminal outcome once. After a bounded failure it follows the
existing failed-execution path.

The orchestrator acknowledges an accepted queue row as today, releases its
dispatch claim, then requests one guarded successor evaluation. A terminal
session uses only existing eligible recovery or primary transfer; otherwise
the remaining queue stays durable with visible recovery state.

## Tests

| Criteria | Focused evidence |
| --- | --- |
| Agent `.1`-.3 | Fake-client tests for unexpected/intentional close, exact-generation reattach, retained terminal outcome, and exhausted retry. |
| Agent `.4`-.5 | Pure channel tests for stage snapshots, a blocked handler with continuing frame receipt, metadata heartbeat exclusion, and one log per prompt generation. |
| Task `.1`-.4 | Existing orchestrator queue recovery tests for accepted-error acknowledgement, successor eligibility, terminal session, Auto-run OFF, concurrent wake, and restart. |

## Work orders

- [ ] [Task 01: Record the first missing update-stream stage](task-01-record-stream-stages.md)
- [ ] [Task 02: Reattach an active prompt after an unexpected stream close](task-02-reattach-active-stream.md)
- [ ] [Task 03: Recover the queue after accepted post-dispatch error](task-03-recover-accepted-queue.md)

Task 02 uses Task 01's stage snapshot. Task 03 is independent in code but
remains sequential in the primary conversation. No subagents are authorized.

## Verification results

Pending implementation. Each work order names changed packages and exact
`go test -p 2 -tags fts5 -run` commands. The owner excluded listener-opening
tests, full Go tests, and E2E. The operator owns the post-build installation
replacement.

## Risks

- A connected stream with a blocked event worker is not repaired by reconnect
  alone; the new first-stall record is necessary to classify that path.
- A replayed terminal outcome and a delayed live frame can race; both must be
  fenced to the original execution and applied once.
- A terminal FAILED session may not be promptable. Queue recovery must keep
  accepted work durable and visible without silently bypassing recovery policy.
