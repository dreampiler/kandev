---
id: "02-drop-structurally-unavailable-entries"
title: "Drop structurally unavailable entries"
status: planned
wave: 2
depends_on:
  - "01-drop-invalid-launch-replays"
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-001.5
  - AC-AGENTS-SESSION-CEILING-001.12
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Task 02: Drop structurally unavailable entries

## Summary

Separate a workflow entry that can never become valid from one whose reads
failed. Drop the first and keep the second for a later sweep.

## In scope

- Add `ceilingEntryPermanentlyUnavailable` and return it for a legacy
  sessionless `start`, an underivable or incomplete binding, an unrouted entry
  whose step now routes sessions, and a missing durable entry identity.
- Return read errors from `deriveCeilingEntryBinding` and add
  `readWorkflowEntryIdentity`, so a failed step, transition, or task read stays
  `unavailable`.
- Treat the new disposition like `superseded` in the sweep's drop reasons, the
  claimed replay load, the post-claim validation, and the claimed binding check.

## Out of scope

- Moves into steps that cannot run an agent. The existing start drop reason
  already covers them.
- An operator route to discard a record.

## Acceptance

- A sweep drops a sessionless `start` or an underivable binding instead of
  retrying it.
- A sweep keeps a record when the step or transition read fails during entry
  validation.

## Verification

```bash
cd apps/backend && go test ./internal/orchestrator/ -run 'CeilingReplay|CeilingEntry|WorkflowEntryIdentity'
```

## Likely files

- `apps/backend/internal/orchestrator/ceiling_entry.go`
- `apps/backend/internal/orchestrator/ceiling_replay.go`
- `apps/backend/internal/orchestrator/workflow_session_target.go`
- `apps/backend/internal/orchestrator/task_operations.go` (call-site signature)
- `apps/backend/internal/orchestrator/ceiling_replay_test.go`

## Results

Not implemented yet.
