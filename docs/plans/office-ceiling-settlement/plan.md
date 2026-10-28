---
created: 2026-10-28
status: implemented
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
legacy_specs: []
---

# Implementation Plan: Settle Office runs across ceiling replay

## Overview

An Office run whose launch the session ceiling refuses is parked as
`blocked_provider_action_required` with no retry deadline. The ceiling replay
sweep is the only launcher that resumes it. Before this plan, nothing settled
the parked Office run when the replay admitted or dropped the deferral, so the
run stayed queued and blocked after its session had already started.

This plan adds one Office-side settlement path driven by the ceiling's own
replay result. It keeps the ceiling as the sole launcher and adds no Office
retry, park status, timed lift, or backfill.

## Problem

- The ceiling start-deferral payload did not carry the Office run identity, so
  the replayed launch lost its Office attribution.
- A successful replay left the parked Office run queued and blocked because
  the Office scheduler deliberately never retries a ceiling-deferred launch.
- A terminal ceiling drop failed the automation run but not the Office run.
- The replay-success settlement write is best-effort. Its `AgentReady`
  recovery and the launch's `AgentStopped`/completion event run in separate
  goroutines; completion can arrive first, find no claimed run, and be
  discarded. A later ready recovery then claims a run whose execution has
  already stopped, with no later event to finish it.

## Scope

### In scope

- Persist and restore the Office `causing_run_id` in the ceiling `start`
  deferral payload.
- Settle the exact parked Office run when the ceiling replay admits or drops
  the deferral, using a compare-and-set on the run id, `queued` status,
  `blocked_provider_action_required`, payload task, and Office agent.
- Recover a failed replay-success settlement from the replayed launch's
  `AgentReady` event, gated by the durable ceiling-deferral marker.
- Finish the parked run directly when the launch's completion event arrives
  before the ready recovery. Completion and ready settlement are mutually
  exclusive exact-row transitions; completion retries the claimed-run lookup
  once if ready won the race.
- Refuse to guess when more than one parked ceiling run matches the same task
  and agent.
- Document the ceiling as the recovery owner and warn against manually
  clearing the routing block.

### Out of scope

- Automatic Office-side retry of a ceiling-deferred run, a session-capacity
  park status, timed lift, or boot backfill. These remain a separate
  policy/contract proposal.
- The taskless/execution-grade capacity work in `#4715` and the explicit
  discard contract in `#4508`.
- The unrelated Vim/formatting and event-read efficiency suggestions on the
  same PR.

## Technical approach

Follow the [session-ceiling system design](../../specs/agents/system-design/session-concurrency-ceiling.md)
and the Office routing design's deferred-launch section
([routing-01](../../specs/office/system-design/routing-01.md)). The Office
service observes the ceiling's replay outcome and performs an exact-row run
transition; it never dispatches a new launch. The completion-before-ready
ordering is a single parked-to-finished transition, so no run is left claimed
after its execution has stopped.

## Work orders

- [Task 01: Settle Office runs across ceiling replay](task-01-settle-office-runs-on-ceiling-replay.md)

## Validation

- Focused Office service and repository tests cover replay attribution,
  exact-row settlement, duplicate/stale no-ops, drop failure, ready recovery,
  completion-before-ready, ready-then-completion, ambiguity refusal, and the
  task/agent/marker guards.
- `go build ./...`, `go vet` on the touched packages, changed-package lint,
  documentation coverage, and normal commit hooks pass on the review head.
