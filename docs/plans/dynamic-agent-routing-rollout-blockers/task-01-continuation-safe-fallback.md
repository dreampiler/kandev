---
id: "dynamic-routing-blockers-01"
title: "Continuation-safe fallback"
status: completed
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-ROLLOUT-BLOCKERS-001
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-ROLLOUT-BLOCKERS-001.2
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-ROLLOUT-BLOCKERS-001.3
system_design:
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
---

# Task 01: Continuation-safe fallback

Add regression coverage for partial output and tool-effect failures, then make
the conductor build, persist, and deliver bounded continuation data. Walk all
eligible candidates with generation fencing and never transfer a provider
native ACP identity across profiles.

**Verification:** dynamic engine and conductor tests plus focused orchestrator
failure tests.

## Results

Completed. Dynamic attempts now require explicit no-output/no-effect evidence;
ambiguous and stale execution events fail closed. The conductor persists and
delivers bounded provider-neutral continuation packages, fences native ACP
identity, and walks every eligible candidate. Focused dynamic and orchestrator
tests passed.

## Queue-owned transcript follow-up

The follow-up preserves queue ownership when a recorded user row is visible
before its queued delivery settles. It changes only new continuation transcript
selection and durable relaunch-prompt reconstruction. Complete queue snapshots
include retained reservations; optional ordinary dispatch persistence covers
rows moved out of the queue table. Read errors stop reconstruction. The existing
writer's deterministic transcript ID is shared without changing its identity.

Runtime scope: `dynamic_launch.go`, `dynamic_queued_transcript.go`, and the ID
extraction in `event_handlers_agent.go`. No schema, public API, new launch mode,
automatic terminal recovery, or change to saved continuation/cache authority.
The combined Native tree already provides the same complete queue/claim helper
in `dynamic_launch_orphan_route.go`, both consumers and the deterministic writer
identity. Keep those existing functions instead of duplicating private helpers.
The existing `provider_interruption_queue_test.go` exercises the two actual
consumers with real SQLite transcript and queue storage, including pending,
reserved, restored, accepted, foreign-session, unrecorded and retained entries,
plus queue/claim read failure and stores without the optional capability.
The upstream PR #4848 current-main baseline fails with duplicated queue-owned rows and treats
queue/claim read errors as empty (1.512s). After the fix, these existing-file
cases and the selected ownership, unknown-delivery, restart and continuation
controls pass with `go test -trimpath -tags fts5 -race -count=1` (10.802s).
Existing transcript-writer provenance and restart-idempotency controls pass
with the same flags (2.822s). Documentation coverage reports `covered`.
These are historical upstream source runs, not this Native integration's result.
This integration records its own focused controls separately. The source runs
do not claim full backend or live-provider validation.
