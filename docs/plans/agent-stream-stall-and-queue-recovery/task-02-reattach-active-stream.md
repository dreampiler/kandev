---
id: "02-reattach-active-stream"
title: "Reattach an active prompt after an unexpected stream close"
status: pending
wave: 2
depends_on:
  - "01-record-stream-stages"
plan: "plan.md"
requirements:
  - REQ-AGENTS-UPDATE-STREAM-CONTINUITY-001
acceptance_criteria:
  - AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.1
  - AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.2
  - AC-AGENTS-UPDATE-STREAM-CONTINUITY-001.3
system_design:
  - ../../specs/agents/system-design/update-stream-continuity.md
---

# Task 02: Reattach an Active Prompt After an Unexpected Stream Close

## Summary

Give an active execution one bounded chance to reconnect its update stream to
the same agentctl instance. Recover the retained terminal outcome if the
prompt finished in the gap; do not send the prompt again.

## In scope

- Change `internal/agent/runtime/lifecycle/streams.go` and
  `manager_events.go` to fence reattachment by execution, startup attempt,
  prompt generation, and intentional-stop state.
- Reuse `agentctl.Client.StreamUpdates` and the retained turn-outcome control
  seam; acknowledge a matching outcome only after it is applied.
- Add pure fake-client tests to existing lifecycle test files for successful
  reattachment, exhausted deadline, late old-reader callback, retained
  outcome/live-frame duplicate, and stop/shutdown exclusion.

## Exclusions

- No provider prompt replay, agent process restart, reconnection on mere
  silence, or broad runtime redesign.

## Implementation acceptance

1. An unexpected mid-turn close can resume event delivery for the same
   execution without a second prompt or session.
2. A matching terminal outcome settles once; stale callbacks cannot settle
   a successor prompt.
3. Intentional teardown is never retried, and a bounded failed reattachment
   follows the existing failure path rather than leaving RUNNING indefinitely.

## Verification

From `apps/backend`, run:

```text
go test -p 2 -tags fts5 -run '^TestActiveUpdateStreamReattach' ./internal/agent/runtime/lifecycle
go test -p 2 -tags fts5 -run '^TestActiveUpdateStreamReattach' ./internal/agent/runtime/agentctl
```

No listener-opening tests, full Go suite, or E2E.

## Dependencies and risks

Task 01 defines the stream-generation snapshot. The current disconnect
callback marks failed; defer that decision only during the bounded valid
reattachment. Preserve the existing retained-outcome and duplicate-frame
guards rather than adding a second terminal state.

## Results

Pending.
