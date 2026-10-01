---
status: draft
system: agents
requirements:
  - REQ-AGENTS-UPDATE-STREAM-CONTINUITY-001
---

# Agent Update Stream Continuity System Design

## Purpose and boundaries

`agentctl.Client.StreamUpdates` reads the per-instance WebSocket and enqueues
agent frames to one ordered handler worker. `StreamManager` opens the stream;
its unexpected-disconnect callback currently signals a prompt error and marks
the execution failed. `dispatchPrompt` can reconnect before a new prompt when
the stream handle is absent, and backend-restart recovery reconnects, but
neither path reattaches a still-active prompt after its update stream drops.
The agent system owns this transport/prompt boundary. The paired task design
owns what happens to queued work after a terminal prompt result.

## Requirement mapping

| Criteria | Sections |
| --- | --- |
| `AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.1` through `.3` | Bounded reattachment and outcome reconciliation |
| `AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.4`, `.5` | Stage diagnostics and liveness |

## Evidence and classification

On 2026-10-01, separate executions on agentctl ports 41007, 41015, and 41001
stopped producing backend turn events around 11:54 KST. Their later stall
publications exceeded the existing 30-second bound and synthetic completions
followed. Local Codex records for the MPM ACP session continued through about
03:11Z, after the backend's last recorded turn activity at 02:54Z. The backend
still received some MCP requests and its persistence health probe remained
healthy. Several earlier `use of closed network connection` lines coincide
with explicit parent stop operations and do not identify the first shared
failure. The same no-event/stall-publication pattern occurred near 08:01, but
the retained logs have no frame-stage or goroutine receipt for either incident.

The historical evidence therefore proves a delivery gap but cannot assign it
to ACP adapter forwarding, agentctl WebSocket writing, backend frame reading,
or the ordered event worker. Do not claim that reattachment alone fixes both
episodes. The diagnostic path below must classify the next one.

## Stage diagnostics and liveness

Track bounded timestamps/counters at four existing seams: agentctl process
manager enqueue from the adapter; agentctl stream writer send; backend
`readUpdatesStream` receipt before its ordered queue; and completion of the
backend dispatch worker's handler. A lightweight control heartbeat on the
same WebSocket carries only agentctl stage counters. It is internal transport
metadata, not an agent event, and must not advance prompt activity. The
backend records heartbeat receipt and stream connectivity without storing
frame bodies.

At the first five-minute stall snapshot in `waitForPromptDone`, emit one
structured line for the execution/prompt generation with the ages of these
stages, pending worker queue depth, stream-connected state, and an explicit
availability flag for counters not received. The log is emitted before
synchronous `agent.stalled` publication, so a blocked stall subscriber cannot
hide the diagnostic. Closed-set stage/outcome fields avoid raw content or
unbounded task labels. A current heartbeat plus stale adapter enqueue points
upstream of agentctl; fresh enqueue plus stale writer points at forwarding;
fresh backend receipt plus stale handler completion points at the worker;
missing heartbeat prevents a claim beyond the transport boundary.

The two historical whole-install incidents both preceded an incomplete stop
request, but their logs end before `StopAgentWithReason` identifies a wait
site. Its phase tracker starts before `remoteInstanceLifecycleMu`, advances
through activity acquisition, agentctl stop, runtime stop, and stopped-event
publication, and records one limited goroutine profile after a 30-second wait.
The first prompt stall shares a process-wide cooldown with that snapshot and
logs its stage counters before synchronous `agent.stalled` publication. The
profile uses stack sites without argument values and is written to a local
temporary file; its path and truncation status are logged. Neither the
historical logs nor the added instrumentation assert that the suspected
lock/event-publication cycle has already occurred.

## Bounded reattachment and outcome reconciliation

On an unexpected WebSocket close, retain the exact execution, startup
generation, and prompt generation before mutating state. First exclude
intentional shutdown, stop, cancellation, credential rotation, and replaced
executions. Try a bounded reattachment to the same agentctl instance. Keep
the in-flight prompt armed; never call `SendPrompt` again. On reattachment,
drain buffered frames in order and inspect the instance's retained terminal
turn outcome through the existing agentctl control-client mechanism. Apply a
matching outcome once and acknowledge it only after application. Existing
generation and turn-outcome duplicate guards reject a later live copy.

If the instance is gone, reattachment expires, or outcome identity cannot be
established, invoke the current execution-failure path once and release its
runtime slot. Do not let a stale reader's deferred disconnect clear a
replacement stream. A socket that remains connected while the handler stalls
is not automatically reattached from silence alone; its stage snapshot first
identifies whether transport or handler work ceased, so a legitimate quiet
tool is not interrupted.

## Verification

Use pure channel/fake-client tests in `internal/agent/runtime/agentctl`,
`internal/agent/runtime/lifecycle`, and `internal/agentctl/server/api` for
heartbeat classification, handler backlog, unexpected close, exact-generation
reattachment, retained outcome, and intentional-stop exclusion. The owner
forbids tests that open listeners, full Go tests, and E2E. No operating
instance change is part of this design turn.
