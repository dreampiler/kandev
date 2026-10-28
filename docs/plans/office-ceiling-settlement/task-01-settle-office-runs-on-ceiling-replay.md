---
id: "01-settle-office-runs-on-ceiling-replay"
title: "Settle Office runs across ceiling replay"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-001.5
  - AC-AGENTS-SESSION-CEILING-001.10
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Task 01: Settle Office runs across ceiling replay

## Summary

Make the parked Office run follow the ceiling replay to a terminal outcome
without becoming a second launcher. The ceiling's stored `start` payload
carries the Office run identity; a successful replay claims and binds the
exact parked row, a terminal drop fails it, and the launch's own ready or
completion event can finish the settlement when the best-effort replay write
failed.

## In scope

- `causing_run_id` round-trip through the ceiling `start` deferral payload and
  replay options.
- Exact-row settlement on replay success (`queued` +
  `blocked_provider_action_required` + payload task + Office agent).
- Exact-row failure on a terminal ceiling drop.
- `AgentReady` recovery gated by the run's latest durable ceiling-deferral
  marker.
- Completion-before-ready ordering: a parked run is finished atomically when
  its completion arrives first; a ready-recovery claim is retried once through
  the normal completion path.
- Ambiguity refusal when more than one parked run matches the same task and
  agent.
- Office routing design and public provider-routing documentation for the
  recovery owner and the manual-clear warning.

## Out of scope

- Office-side automatic retry, `waiting_for_session_capacity`, timed lift, or
  boot backfill.
- The taskless/execution-grade capacity policy (`#4715`), the explicit discard
  contract (`#4508`), and the unrelated optional review suggestions on the
  same PR.

## Acceptance

- `AC-AGENTS-SESSION-CEILING-001.5`: the replay sweep remains the only replay
  owner; the Office run is settled from its stored identity without adding a
  second retry path.
- `AC-AGENTS-SESSION-CEILING-001.10`: an admitted replay binds its session to
  the run, a dropped start fails the run, and no run is left claimed after its
  execution has stopped.

## Changed paths

- `apps/backend/internal/office/repository/sqlite/run_routing.go`
- `apps/backend/internal/office/service/ceiling_deferred_settlement.go`
- `apps/backend/internal/office/service/event_subscribers.go`
- `apps/backend/internal/office/service/taskless_completion.go`
- `apps/backend/internal/orchestrator/ceiling_seams.go`
- `apps/backend/internal/orchestrator/ceiling_replay.go`
- `apps/backend/internal/orchestrator/ceiling_replay_result.go`
- `apps/backend/internal/orchestrator/service.go`
- `apps/backend/internal/backendapp/main.go`
- `docs/specs/office/system-design/routing-01.md`
- `docs/public/office-provider-routing.md`

## Validation

- `go build ./...`; focused Office service/repository and orchestrator tests
  cover replay attribution, settlement and drop, duplicate/stale no-ops,
  ready recovery, completion-before-ready, ready-then-completion, ambiguity,
  and mismatched task/agent/marker no-ops.
- Changed-package lint, documentation coverage, and normal commit hooks pass on
  the review head.
